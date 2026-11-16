using System.Diagnostics;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Text.Json;
using Microsoft.Web.WebView2.Core;
using Microsoft.Win32;
using Windows.Win32;
using Windows.Win32.Foundation;
using Windows.Win32.Graphics.Dwm;
using Windows.Win32.System.Com;
using Windows.Win32.System.Com.StructuredStorage;
using Windows.Win32.System.Variant;
using Windows.Win32.UI.Controls;
using Windows.Win32.UI.Shell;
using Windows.Win32.UI.Shell.Common;
using Windows.Win32.UI.Shell.PropertiesSystem;
using Windows.Win32.UI.WindowsAndMessaging;

namespace Aetheris;

// The app's window: the interface in ui/ (Svelte, built from the web UI's design system) in WebView2, its title
// bar in the page's own colour. It talks to the engine in JSON messages. Closing it frees the browser engine;
// the app carries on in the tray.
static class AppWindow
{
    public static HWND Hwnd;
    static CoreWebView2Environment? env;
    static CoreWebView2Controller? view;
    static Profile profile = null!;
    static bool pushQueued;
    static long lastPush;
    static (long Bytes, int Files, int Online, int Pinned, long At) storage;
    static bool dark;
    static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web);

    public static unsafe void Show(Profile p)
    {
        profile = p;
        if (Hwnd != default)
        {
            if (PInvoke.IsIconic(Hwnd))
                PInvoke.ShowWindow(Hwnd, SHOW_WINDOW_CMD.SW_RESTORE);
            PInvoke.SetForegroundWindow(Hwnd);
            return;
        }
        uint dpi = PInvoke.GetDpiForSystem();
        int S(int v) => v * (int)dpi / 96;
        int w = S(460), h = S(720);
        fixed (char* cls = "AetherisApp")
        fixed (char* title = p.DisplayName)
        {
            var wc = new WNDCLASSEXW
            {
                cbSize = (uint)sizeof(WNDCLASSEXW),
                lpfnWndProc = &WndProc,
                hInstance = Tray.Instance,
                lpszClassName = cls,
                hIcon = Tray.Icon,
                hCursor = PInvoke.LoadCursor(default, PInvoke.IDC_ARROW),
            };
            PInvoke.RegisterClassEx(&wc); // fails harmlessly the second time
            Hwnd = PInvoke.CreateWindowEx(0, cls, title, WINDOW_STYLE.WS_OVERLAPPEDWINDOW,
                (PInvoke.GetSystemMetrics(SYSTEM_METRICS_INDEX.SM_CXSCREEN) - w) / 2, (PInvoke.GetSystemMetrics(SYSTEM_METRICS_INDEX.SM_CYSCREEN) - h) / 2,
                w, h, default, default, Tray.Instance, null);
        }
        Theme();
        try { TaskbarIcon(p); }
        catch (Exception e) { Log.Error($"taskbar icon: {e.Message}"); }
        PInvoke.ShowWindow(Hwnd, SHOW_WINDOW_CMD.SW_SHOW);
        PInvoke.SetForegroundWindow(Hwnd);
        _ = Start();
    }

    // With the package identity, Windows draws the window's taskbar button from the package's logo, and it came out as
    // a blank tile. The window gets an app ID of its own instead, so the taskbar shows the window's icon: the exe's.
    static unsafe void TaskbarIcon(Profile p)
    {
        HICON big, small;
        fixed (char* exe = Environment.ProcessPath)
            PInvoke.SHDefExtractIcon(exe, 0, 0, &big, &small, 16 << 16 | 48); // 48 px for the taskbar, 16 for the title bar
        PInvoke.SendMessage(Hwnd, PInvoke.WM_SETICON, PInvoke.ICON_BIG, (nint)big.Value);
        PInvoke.SendMessage(Hwnd, PInvoke.WM_SETICON, PInvoke.ICON_SMALL, (nint)small.Value);
        var iid = typeof(IPropertyStore).GUID;
        void* raw;
        PInvoke.SHGetPropertyStoreForWindow(Hwnd, &iid, &raw).ThrowOnFailure();
        var store = (IPropertyStore*)raw;
        try
        {
            fixed (char* id = p.AppId)
            {
                var value = new PROPVARIANT();
                value.vt = VARENUM.VT_LPWSTR;
                value.pwszVal = id;
                var key = PInvoke.PKEY_AppUserModel_ID;
                store->SetValue(&key, &value);
                store->Commit();
            }
        }
        finally
        {
            store->Release();
        }
    }

    // Windows' app theme picks light or dark. The title bar takes the page's background (Seashell / Shadow grey)
    // instead of the accent colour, so window and content read as one surface; the page is told the same theme.
    // (Mica was tried: with "accent colour on title bars" or transparency off it left a white page under dark text.)
    static unsafe void Theme()
    {
        using (var k = Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize"))
            dark = k?.GetValue("AppsUseLightTheme") is 0;
        int on = dark ? 1 : 0;
        uint bg = dark ? 0x00241E1Eu : 0x00E8EBF7u, text = dark ? 0x00E8EBF7u : 0x00241E1Eu; // COLORREF is 0x00BBGGRR
        PInvoke.DwmSetWindowAttribute(Hwnd, DWMWINDOWATTRIBUTE.DWMWA_USE_IMMERSIVE_DARK_MODE, &on, sizeof(int));
        PInvoke.DwmSetWindowAttribute(Hwnd, DWMWINDOWATTRIBUTE.DWMWA_CAPTION_COLOR, &bg, sizeof(uint));
        PInvoke.DwmSetWindowAttribute(Hwnd, DWMWINDOWATTRIBUTE.DWMWA_BORDER_COLOR, &bg, sizeof(uint));
        PInvoke.DwmSetWindowAttribute(Hwnd, DWMWINDOWATTRIBUTE.DWMWA_TEXT_COLOR, &text, sizeof(uint));
        if (view != null)
        {
            view.DefaultBackgroundColor = Background();
            view.CoreWebView2.Profile.PreferredColorScheme = dark ? CoreWebView2PreferredColorScheme.Dark : CoreWebView2PreferredColorScheme.Light;
        }
    }

    static System.Drawing.Color Background() => dark ? System.Drawing.Color.FromArgb(0x1E, 0x1E, 0x24) : System.Drawing.Color.FromArgb(0xF7, 0xEB, 0xE8);

    static async Task Start()
    {
        try
        {
            var data = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Aetheris", "webview");
            env ??= await CoreWebView2Environment.CreateAsync(null, data);
            var hwnd = Hwnd;
            var v = await env.CreateCoreWebView2ControllerAsync(Ptr(hwnd));
            if (Hwnd != hwnd) // closed while starting
            {
                v.Close();
                return;
            }
            view = v;
            v.DefaultBackgroundColor = Background(); // no white flash before the page paints
            Resize();
            var wv = v.CoreWebView2;
            wv.Profile.PreferredColorScheme = dark ? CoreWebView2PreferredColorScheme.Dark : CoreWebView2PreferredColorScheme.Light;
            wv.Settings.AreDevToolsEnabled = profile.Dev;
            wv.Settings.AreDefaultContextMenusEnabled = profile.Dev;
            wv.Settings.IsStatusBarEnabled = false;
            wv.Settings.IsZoomControlEnabled = false;
            wv.Settings.AreBrowserAcceleratorKeysEnabled = false; // no reload, find or print: it's an app, not a page
            wv.Settings.IsPasswordAutosaveEnabled = false;
            wv.Settings.IsGeneralAutofillEnabled = false;
            wv.SetVirtualHostNameToFolderMapping("app.aetheris", Path.Combine(AppContext.BaseDirectory, "ui"), CoreWebView2HostResourceAccessKind.DenyCors);
            // The window only ever shows its own pages; links go to the default browser.
            wv.NavigationStarting += (_, e) =>
            {
                if (!e.Uri.StartsWith("https://app.aetheris/"))
                {
                    e.Cancel = true;
                    Open(e.Uri);
                }
            };
            wv.NewWindowRequested += (_, e) =>
            {
                e.Handled = true;
                Open(e.Uri);
            };
            wv.WebMessageReceived += (_, e) => Receive(e.WebMessageAsJson);
            wv.Navigate("https://app.aetheris/index.html");
        }
        catch (Exception e)
        {
            Log.Error($"app window: {e.Message}");
            PInvoke.DestroyWindow(Hwnd); // no WebView2 runtime, say: the tray still works
        }
    }

    static unsafe IntPtr Ptr(HWND h) => (IntPtr)h.Value;

    static void Resize()
    {
        if (view == null)
            return;
        PInvoke.GetClientRect(Hwnd, out var r);
        view.Bounds = new System.Drawing.Rectangle(0, 0, r.right - r.left, r.bottom - r.top);
    }

    static void Open(string target) => Process.Start(new ProcessStartInfo(target) { UseShellExecute = true })?.Dispose();

    // Engine changes arrive in bursts (every MB of a transfer): at most ten updates a second reach the page.
    public static void Push()
    {
        if (view == null || pushQueued)
            return;
        long wait = 100 - (Environment.TickCount64 - lastPush);
        if (wait > 0)
        {
            pushQueued = true;
            _ = Task.Delay((int)wait).ContinueWith(_ =>
            {
                pushQueued = false;
                PInvoke.PostMessage(Tray.Hwnd, Tray.WM_STATUS, 0, 0);
            });
            return;
        }
        lastPush = Environment.TickCount64;
        if (Environment.TickCount64 - storage.At > 5000)
        {
            storage.At = Environment.TickCount64;
            _ = Task.Run(() =>
            {
                storage = Engine.Storage(profile.Root) is var s ? (s.Bytes, s.Files, s.Online, s.Pinned, Environment.TickCount64) : storage;
                PInvoke.PostMessage(Tray.Hwnd, Tray.WM_STATUS, 0, 0);
            });
        }
        Send(new
        {
            type = "state",
            name = profile.DisplayName,
            host = profile.Server?.Host, // also what the setup offers again after a sign-out
            home = profile.Lan.FirstOrDefault(),
            web = profile.Web,
            route = Nas.Route,
            paired = Engine.Phase != Phase.NotPaired,
            phase = Engine.Phase.ToString(),
            status = Tray.Status(),
            detail = Engine.Detail,
            lastSync = Engine.LastSync,
            autostart = profile.Autostart,
            drive = new { letter = Drive.Letter?.ToString(), free = Drive.Free().Select(c => c.ToString()) },
            storage = new { bytes = storage.Bytes, files = storage.Files, online = storage.Online, pinned = storage.Pinned },
            transfers = SyncRoot.Transfers().Concat(Local.Transfer() is { } up ? [up] : Array.Empty<Engine.Transfer>()),
            activity = Engine.Recent(),
            computer = Engine.Host,
            folders = profile.Folders.Select(f => new { name = f.Name, path = f.Local, here = SyncRoot.Folders.Contains(f) }),
            suggested = Suggested(),
            version = Setup.Shown,
            update = Update.Pending,
            autoFree = profile.AutoFreeDays,
            recordsOpens = Engine.RecordsOpens(),
            longUpload = Local.LongUpload() is { } big ? new { left = big.Left?.TotalSeconds, files = big.Files, bytes = big.Bytes, done = big.Done } : null,
        });
    }

    // Your usual folders, offered in the setup while they aren't synced yet.
    static IEnumerable<object> Suggested() =>
        new[] { Environment.SpecialFolder.DesktopDirectory, Environment.SpecialFolder.MyDocuments, Environment.SpecialFolder.MyPictures }
            .Select(Environment.GetFolderPath)
            .Where(path => Directory.Exists(path) && !profile.Folders.Exists(f => f.Local.Equals(path, StringComparison.OrdinalIgnoreCase)))
            .Select(path => new { name = Path.GetFileName(path), path });

    // Windows' own folder picker, over this window.
    static unsafe string? PickFolder()
    {
        PInvoke.CoCreateInstance(typeof(FileOpenDialog).GUID, null, CLSCTX.CLSCTX_INPROC_SERVER, out IFileOpenDialog* dialog).ThrowOnFailure();
        try
        {
            dialog->SetOptions(FILEOPENDIALOGOPTIONS.FOS_PICKFOLDERS | FILEOPENDIALOGOPTIONS.FOS_FORCEFILESYSTEM | FILEOPENDIALOGOPTIONS.FOS_PATHMUSTEXIST);
            fixed (char* title = "Choose a folder to sync")
                dialog->SetTitle(title);
            try { dialog->Show(Hwnd); }
            catch (COMException e) when (e.HResult == unchecked((int)0x800704C7)) { return null; } // cancelled
            IShellItem* item;
            dialog->GetResult(&item);
            try
            {
                PWSTR path;
                item->GetDisplayName(SIGDN.SIGDN_FILESYSPATH, &path);
                try { return path.ToString(); }
                finally { PInvoke.CoTaskMemFree(path); }
            }
            finally
            {
                item->Release();
            }
        }
        finally
        {
            dialog->Release();
        }
    }

    static void Send(object message) => view?.CoreWebView2.PostWebMessageAsJson(JsonSerializer.Serialize(message, Json));

    static async void Receive(string json)
    {
        try
        {
            using var doc = JsonDocument.Parse(json);
            var m = doc.RootElement;
            switch (m.GetProperty("type").GetString())
            {
                case "ready":
                    storage.At = 0; // fresh numbers for a fresh window
                    break;
                case "pair":
                    string Text(string name) => m.TryGetProperty(name, out var v) ? v.GetString() ?? "" : "";
                    var error = await Engine.Pair(profile, Text("server"), Text("home"), Text("code"));
                    Send(new { type = "paired", error });
                    if (error == null)
                        Tray.Paired();
                    break;
                case "pause":
                    Engine.Pause();
                    break;
                case "resume":
                    Engine.Resume();
                    break;
                case "autostart":
                    profile.Autostart = m.GetProperty("on").GetBoolean();
                    break;
                case "drive":
                    var letter = m.GetProperty("letter").GetString();
                    Drive.Map(profile, letter is { Length: 1 } ? letter[0] : null);
                    break;
                case "free":
                    long freed = await Task.Run(() => Engine.FreeUpSpace(profile.Root));
                    storage.At = 0;
                    Send(new { type = "freed", bytes = freed });
                    break;
                case "open":
                    var path = m.TryGetProperty("path", out var p) ? p.GetString() : null;
                    var target = m.GetProperty("what").GetString() switch
                    {
                        "drive" when Drive.Letter is { } c => $"{c}:\\",
                        "web" => profile.Web,
                        "log" => profile.LogFile,
                        "computer" when profile.Web != null => $"{profile.Web}/#/files/{Folder.Computers}/{Uri.EscapeDataString(Engine.Host)}",
                        "computer" => null,
                        _ => profile.Folders.Find(f => f.Local == path)?.Local ?? profile.Root, // only folders that sync
                    };
                    if (target != null)
                        Open(target);
                    break;
                case "autoFree":
                    int days = m.GetProperty("days").GetInt32();
                    profile.AutoFreeDays = days;
                    if (days > 0)
                    {
                        storage.At = 0;
                        _ = Task.Run(() => Engine.AutoFree(days));
                    }
                    break;
                case "addFolder":
                    var chosen = m.TryGetProperty("path", out var given) ? given.GetString() : PickFolder();
                    if (chosen != null)
                        Send(new { type = "folder", error = await Engine.AddFolder(profile, chosen) });
                    break;
                case "removeFolder":
                    var folder = profile.Folders.Find(f => f.Local == m.GetProperty("path").GetString());
                    if (folder != null)
                        Send(new { type = "folder", error = await Engine.RemoveFolder(profile, folder) });
                    break;
                case "signout":
                    await Engine.SignOut(profile);
                    break;
            }
        }
        catch (Exception e)
        {
            Log.Error($"app window: {e.Message}");
            Send(new { type = "error", message = e.Message });
        }
        Push();
    }

    [UnmanagedCallersOnly(CallConvs = [typeof(CallConvStdcall)])]
    static unsafe LRESULT WndProc(HWND hwnd, uint msg, WPARAM wParam, LPARAM lParam)
    {
        try
        {
            switch (msg)
            {
                case PInvoke.WM_SIZE:
                    Resize();
                    return default;
                case PInvoke.WM_MOVE:
                    view?.NotifyParentWindowPositionChanged();
                    return default;
                case PInvoke.WM_SETFOCUS:
                    view?.MoveFocus(CoreWebView2MoveFocusReason.Programmatic);
                    return default;
                case PInvoke.WM_DPICHANGED: // the window keeps its physical size on a screen with other scaling
                    var r = (RECT*)lParam.Value;
                    PInvoke.SetWindowPos(hwnd, default, r->left, r->top, r->right - r->left, r->bottom - r->top,
                        SET_WINDOW_POS_FLAGS.SWP_NOZORDER | SET_WINDOW_POS_FLAGS.SWP_NOACTIVATE);
                    return default;
                case PInvoke.WM_GETMINMAXINFO:
                    uint dpi = PInvoke.GetDpiForWindow(hwnd);
                    var mm = (MINMAXINFO*)lParam.Value;
                    mm->ptMinTrackSize.X = (int)(380 * dpi / 96);
                    mm->ptMinTrackSize.Y = (int)(540 * dpi / 96);
                    return default;
                case PInvoke.WM_SETTINGCHANGE:
                    Theme();
                    break;
                case PInvoke.WM_DESTROY:
                    view?.Close();
                    view = null;
                    Hwnd = default;
                    return default;
            }
        }
        catch (Exception e)
        {
            Log.Error($"app window: {e.Message}");
        }
        return PInvoke.DefWindowProc(hwnd, msg, wParam, lParam);
    }
}
