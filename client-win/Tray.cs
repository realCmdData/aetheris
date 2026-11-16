using System.Collections.Concurrent;
using System.Diagnostics;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using Windows.Win32;
using Windows.Win32.Foundation;
using Windows.Win32.UI.Shell;
using Windows.Win32.UI.WindowsAndMessaging;

namespace Aetheris;

// The tray icon and its menu; a click opens the app window. Its thread runs the message loop that WebView2 needs.
static unsafe class Tray
{
    public const uint WM_TRAY = PInvoke.WM_APP + 1, WM_STATUS = PInvoke.WM_APP + 2, WM_ALERT = PInvoke.WM_APP + 3, WM_WORK = PInvoke.WM_APP + 4;
    enum Cmd : uint { None, App, Folder, Web, Pause, Autostart, Quit }

    public static HWND Hwnd;
    public static HICON Icon;
    public static HINSTANCE Instance;
    static Profile profile = null!;
    static uint taskbarCreated;
    static string lastTip = "";
    static readonly ConcurrentQueue<string> alerts = new();

    public static int Run(Profile p, bool show)
    {
        // Autostart and a manual start must not both connect the folder.
        using var single = new Mutex(true, $@"Local\Aetheris-{p.Name}", out bool first);
        if (!first)
            return 0;
        profile = p;
        Instance = (HINSTANCE)(nint)PInvoke.GetModuleHandle(default(PCWSTR)).Value;
        HICON small;
        fixed (char* exe = Environment.ProcessPath)
            PInvoke.ExtractIconEx(exe, 0, null, &small, 1);
        Icon = small;
        taskbarCreated = PInvoke.RegisterWindowMessage("TaskbarCreated");
        fixed (char* cls = "AetherisTray")
        fixed (char* title = p.DisplayName) // the installer finds the running copy by this title
        {
            var wc = new WNDCLASSEXW { cbSize = (uint)sizeof(WNDCLASSEXW), lpfnWndProc = &WndProc, hInstance = Instance, lpszClassName = cls };
            PInvoke.RegisterClassEx(&wc);
            // A hidden top-level window, not a message-only one: only those see the TaskbarCreated broadcast.
            Hwnd = PInvoke.CreateWindowEx(0, cls, title, 0, 0, 0, 0, 0, default, default, Instance, null);
        }
        AddIcon();
        SynchronizationContext.SetSynchronizationContext(new LoopContext()); // awaits in the window come back to this thread
        Engine.Changed += () => PInvoke.PostMessage(Hwnd, WM_STATUS, 0, 0);
        Engine.Alert += text =>
        {
            alerts.Enqueue(text);
            PInvoke.PostMessage(Hwnd, WM_ALERT, 0, 0);
        };
        if (p.Autostart)
            p.Autostart = true; // keep the entry pointing at this exe
        if (!p.Dev && Setup.Installed(p))
        {
            Uninstall.Advertise(); // Settings > Apps lists Aetheris, with an uninstall that cleans up properly
            if (!File.Exists(Setup.StartMenuLink(p))) // e.g. an install that came by update: Start and search find it
            {
                try { Setup.Shortcut(p, Setup.StartMenuLink(p)); }
                catch (Exception e) { Log.Error($"Start menu entry: {e.Message}"); }
            }
        }
        _ = Task.Run(() => Engine.Start(p)); // the engine never runs on this thread
        _ = Task.Run(() => Update.Run(p));
        if (p.Token == null || show)
            AppWindow.Show(p); // first run: the setup (pairing, then folders)

        MSG msg;
        while (PInvoke.GetMessage(&msg, default, 0, 0).Value > 0)
        {
            PInvoke.TranslateMessage(&msg);
            PInvoke.DispatchMessage(&msg);
        }
        Engine.Stop();
        return 0;
    }

    public static string Status()
    {
        if (Local.LongUpload() is { } big)
            return big.Left is { } left ? $"Long upload in progress · about {Eta(left)} left" : "Long upload in progress";
        if (Local.Current is { } up)
            return $"Uploading {Path.GetFileName(up)}…";
        int n = SyncRoot.Downloads;
        if (n > 0)
            return n == 1 ? "Downloading 1 file…" : $"Downloading {n} files…";
        return Engine.Phase switch
        {
            Phase.NotPaired => "Not paired with your NAS",
            Phase.Starting or Phase.Syncing => "Checking for changes…",
            Phase.UpToDate => "Up to date",
            Phase.Offline => "Can't reach your NAS · retrying",
            Phase.Paused => "Paused",
            _ => "Stopped by an error",
        };
    }

    public static string Eta(TimeSpan t) =>
        t.TotalMinutes < 1 ? "a minute" : t.TotalHours < 1 ? $"{Math.Ceiling(t.TotalMinutes)} min" : $"{(int)t.TotalHours} h {t.Minutes} min";

    // After a successful pairing (on the UI thread). The app window goes on with the setup's folders step.
    public static void Paired()
    {
        if (!profile.Dev)
            profile.Autostart = true; // a sync client starts with Windows unless you turn that off
        Task.Run(() => Engine.Start(profile));
    }

    static void OpenFolder() => Process.Start("explorer.exe", $"\"{profile.Root}\"")?.Dispose();

    static string Clip(string s, int max) => s.Length <= max ? s : s[..(max - 1)] + "…";

    static NOTIFYICONDATAW Data(NOTIFY_ICON_DATA_FLAGS flags) => new()
    {
        cbSize = (uint)sizeof(NOTIFYICONDATAW),
        hWnd = Hwnd,
        uID = 1,
        uFlags = flags,
        uCallbackMessage = WM_TRAY,
        hIcon = Icon,
        szTip = Clip($"{profile.DisplayName}\n{Status()}", 127),
    };

    static void AddIcon()
    {
        var nid = Data(NOTIFY_ICON_DATA_FLAGS.NIF_MESSAGE | NOTIFY_ICON_DATA_FLAGS.NIF_ICON | NOTIFY_ICON_DATA_FLAGS.NIF_TIP | NOTIFY_ICON_DATA_FLAGS.NIF_SHOWTIP);
        PInvoke.Shell_NotifyIcon(NOTIFY_ICON_MESSAGE.NIM_ADD, &nid);
        nid.uVersion = PInvoke.NOTIFYICON_VERSION_4;
        PInvoke.Shell_NotifyIcon(NOTIFY_ICON_MESSAGE.NIM_SETVERSION, &nid);
    }

    // Shows as a Windows notification. Only for things worth interrupting you: sign-outs and conflicts.
    static void Balloon(string text)
    {
        var nid = Data(NOTIFY_ICON_DATA_FLAGS.NIF_INFO);
        nid.szInfoTitle = profile.DisplayName;
        nid.szInfo = Clip(text, 255);
        nid.dwInfoFlags = NOTIFY_ICON_INFOTIP_FLAGS.NIIF_WARNING;
        PInvoke.Shell_NotifyIcon(NOTIFY_ICON_MESSAGE.NIM_MODIFY, &nid);
    }

    static void Item(HMENU m, Cmd id, string text, MENU_ITEM_FLAGS flags = 0)
    {
        fixed (char* t = text)
            PInvoke.AppendMenu(m, MENU_ITEM_FLAGS.MF_STRING | flags, (nuint)id, t);
    }

    static void Separator(HMENU m) => PInvoke.AppendMenu(m, MENU_ITEM_FLAGS.MF_SEPARATOR, 0, default);

    static void Menu(int x, int y)
    {
        var m = PInvoke.CreatePopupMenu();
        Item(m, Cmd.None, Status(), MENU_ITEM_FLAGS.MF_GRAYED);
        if (Engine.Phase == Phase.Failed) // offline needs no explaining; the details are in the log
            Item(m, Cmd.None, Clip(Engine.Detail, 50), MENU_ITEM_FLAGS.MF_GRAYED);
        Separator(m);
        Item(m, Cmd.App, Engine.Phase == Phase.NotPaired ? "&Pair this PC…" : "Open &Aetheris");
        Item(m, Cmd.Folder, "&Open Aetheris folder");
        if (profile.Web is { } web)
            Item(m, Cmd.Web, $"Open &{new Uri(web).Host}");
        Separator(m);
        if (Engine.Phase is Phase.Syncing or Phase.UpToDate or Phase.Offline or Phase.Paused)
            Item(m, Cmd.Pause, Engine.Phase == Phase.Paused ? "&Resume syncing" : "&Pause syncing");
        Item(m, Cmd.Autostart, "Start with &Windows", profile.Autostart ? MENU_ITEM_FLAGS.MF_CHECKED : 0);
        Separator(m);
        Item(m, Cmd.Quit, "&Quit Aetheris");

        PInvoke.SetForegroundWindow(Hwnd); // otherwise the menu stays open when you click elsewhere
        var align = PInvoke.GetSystemMetrics(SYSTEM_METRICS_INDEX.SM_MENUDROPALIGNMENT) != 0
            ? TRACK_POPUP_MENU_FLAGS.TPM_RIGHTALIGN : TRACK_POPUP_MENU_FLAGS.TPM_LEFTALIGN;
        var cmd = (Cmd)PInvoke.TrackPopupMenu(m, TRACK_POPUP_MENU_FLAGS.TPM_RETURNCMD | TRACK_POPUP_MENU_FLAGS.TPM_RIGHTBUTTON
            | TRACK_POPUP_MENU_FLAGS.TPM_BOTTOMALIGN | align, x, y, 0, Hwnd, null).Value;
        PInvoke.PostMessage(Hwnd, PInvoke.WM_NULL, 0, 0);
        PInvoke.DestroyMenu(m);

        switch (cmd)
        {
            case Cmd.App: AppWindow.Show(profile); break;
            case Cmd.Folder: OpenFolder(); break;
            case Cmd.Web: Process.Start(new ProcessStartInfo(profile.Web!) { UseShellExecute = true })?.Dispose(); break;
            case Cmd.Pause when Engine.Phase == Phase.Paused: Engine.Resume(); break;
            case Cmd.Pause: Engine.Pause(); break;
            case Cmd.Autostart: profile.Autostart = !profile.Autostart; break;
            case Cmd.Quit: PInvoke.DestroyWindow(Hwnd); break;
        }
    }

    [UnmanagedCallersOnly(CallConvs = [typeof(CallConvStdcall)])]
    static LRESULT WndProc(HWND hwnd, uint msg, WPARAM wParam, LPARAM lParam)
    {
        try
        {
            switch (msg)
            {
                case WM_TRAY: // NOTIFYICON_VERSION_4: the event in the low word, the anchor point in wParam
                    switch ((uint)(lParam.Value & 0xFFFF))
                    {
                        case PInvoke.NIN_SELECT: AppWindow.Show(profile); break;
                        case PInvoke.WM_CONTEXTMENU: Menu((short)(wParam.Value & 0xFFFF), (short)((wParam.Value >> 16) & 0xFFFF)); break;
                    }
                    return default;
                case WM_STATUS:
                    var tip = Data(NOTIFY_ICON_DATA_FLAGS.NIF_TIP | NOTIFY_ICON_DATA_FLAGS.NIF_SHOWTIP);
                    if (tip.szTip.ToString() != lastTip) // progress comes every MB; the tooltip only changes now and then
                    {
                        lastTip = tip.szTip.ToString();
                        PInvoke.Shell_NotifyIcon(NOTIFY_ICON_MESSAGE.NIM_MODIFY, &tip);
                    }
                    AppWindow.Push();
                    return default;
                case WM_ALERT:
                    while (alerts.TryDequeue(out var text))
                        Balloon(text);
                    return default;
                case WM_WORK:
                    LoopContext.Drain();
                    return default;
                case PInvoke.WM_DESTROY:
                    if (AppWindow.Hwnd != default)
                        PInvoke.DestroyWindow(AppWindow.Hwnd);
                    var gone = Data(0);
                    PInvoke.Shell_NotifyIcon(NOTIFY_ICON_MESSAGE.NIM_DELETE, &gone);
                    PInvoke.PostQuitMessage(0);
                    return default;
            }
            if (msg == taskbarCreated) // Explorer restarted and forgot the icon
                AddIcon();
        }
        catch (Exception e)
        {
            Log.Error($"tray: {e}");
        }
        return PInvoke.DefWindowProc(hwnd, msg, wParam, lParam);
    }
}

// Runs async continuations on the tray thread through its message loop (WebView2 must stay on its own thread).
sealed class LoopContext : SynchronizationContext
{
    static readonly ConcurrentQueue<(SendOrPostCallback Work, object? State)> queue = new();

    public override void Post(SendOrPostCallback d, object? state)
    {
        queue.Enqueue((d, state));
        PInvoke.PostMessage(Tray.Hwnd, Tray.WM_WORK, 0, 0);
    }

    public static void Drain()
    {
        while (queue.TryDequeue(out var w))
            w.Work(w.State);
    }
}
