using System.Diagnostics;
using System.Reflection;
using Microsoft.Win32;
using Windows.Win32;
using Windows.Win32.Foundation;
using Windows.Win32.System.Com;
using Windows.Win32.System.Com.StructuredStorage;
using Windows.Win32.System.Variant;
using Windows.Win32.UI.Controls;
using Windows.Win32.UI.Shell;
using Windows.Win32.UI.Shell.PropertiesSystem;
using Windows.Win32.UI.WindowsAndMessaging;

namespace Aetheris;

// The released exe (AetherisSetup.exe) is the app and its installer in one. Started from anywhere but the install
// folder (%LOCALAPPDATA%\Programs\Aetheris: per Windows user, no admin rights), it copies itself there and starts that
// copy, whose window then walks through the first-time setup. The installed copy unpacks the app window's files, the
// Explorer package and the uninstaller next to itself whenever it is a new build (after an update, say).
static class Setup
{
    static readonly Assembly self = typeof(Setup).Assembly;

    // From the release tag (v1.2.3 → 1.2.3, set by .github/workflows/release.yml). Local builds are 0.0.0.
    public static Version Version { get; } = Normal(self.GetName().Version!);
    public static string Shown => $"{Version.Major}.{Version.Minor}.{Version.Build}";
    public static bool Released => Version > new Version(0, 0, 0, 0);
    public static Version Normal(Version v) => new(v.Major, v.Minor, Math.Max(v.Build, 0), 0);

    // Release builds carry their files inside (Aetheris.csproj); Debug builds run from their build folder.
    public static bool Packed => self.GetManifestResourceNames().Any(n => n.StartsWith(@"ui\"));

    public static bool Installed(Profile p) =>
        string.Equals(Path.GetDirectoryName(Environment.ProcessPath), p.InstallDir, StringComparison.OrdinalIgnoreCase);

    // Asks first (unless quiet), quits the installed copy, puts this exe in its place, adds the Start menu entry
    // (and a desktop one if wanted) and starts it with its window open. Runs on an STA thread (COM, the dialog).
    public static int Install(Profile p, bool quiet)
    {
        try
        {
            var exe = Path.Combine(p.InstallDir, "Aetheris.exe");
            bool desktop = false;
            if (!quiet)
            {
                if (Ask(p, update: File.Exists(exe)) is not { } wantsDesktop)
                    return 1; // cancelled
                desktop = wantsDesktop;
            }
            Directory.CreateDirectory(p.InstallDir);
            Quit(p);
            for (int i = 0; ; i++)
            {
                try
                {
                    File.Copy(Environment.ProcessPath!, exe, overwrite: true);
                    break;
                }
                catch (IOException) when (i < 20)
                {
                    Thread.Sleep(500); // the old copy is still letting go of its exe
                }
            }
            Unpack(p);
            Shortcut(p, StartMenuLink(p));
            if (desktop)
                Shortcut(p, DesktopLink(p));
            Process.Start(new ProcessStartInfo(exe, p.Dev ? "--dev --show" : "--show") { WorkingDirectory = p.InstallDir })?.Dispose();
            Log.Info($"installed {Shown} in {p.InstallDir}");
            return 0;
        }
        catch (Exception e)
        {
            Log.Error($"install: {e}");
            PInvoke.MessageBox(default, $"Aetheris couldn't be installed.\n\n{e.Message}", "Aetheris", MESSAGEBOX_STYLE.MB_ICONERROR);
            return 1;
        }
    }

    // Windows' own setup dialog: what, for whom and where, with a desktop shortcut to tick. null: cancelled.
    static unsafe bool? Ask(Profile p, bool update)
    {
        const int Go = 100;
        HICON icon;
        fixed (char* exe = Environment.ProcessPath)
            PInvoke.ExtractIconEx(exe, 0, &icon, null, 1);
        fixed (char* title = $"{p.DisplayName} Setup")
        fixed (char* main = update ? $"Update {p.DisplayName} to {Shown}" : $"Install {p.DisplayName} {Shown}")
        fixed (char* content = $"For you ({Environment.UserName}) on this PC, without admin rights. {p.DisplayName} will be in the Start menu; " +
            $"its files go to {p.InstallDir}.")
        fixed (char* label = update ? "Update" : "Install")
        fixed (char* desktop = "Add a shortcut to the desktop")
        {
            var button = new TASKDIALOG_BUTTON { nButtonID = Go, pszButtonText = label };
            var config = new TASKDIALOGCONFIG
            {
                cbSize = (uint)sizeof(TASKDIALOGCONFIG),
                dwFlags = TASKDIALOG_FLAGS.TDF_ALLOW_DIALOG_CANCELLATION | TASKDIALOG_FLAGS.TDF_USE_HICON_MAIN,
                dwCommonButtons = TASKDIALOG_COMMON_BUTTON_FLAGS.TDCBF_CANCEL_BUTTON,
                pszWindowTitle = title,
                pszMainInstruction = main,
                pszContent = content,
                cButtons = 1,
                pButtons = &button,
                nDefaultButton = Go,
                pszVerificationText = update ? default : desktop,
            };
            config.Anonymous1.hMainIcon = icon;
            PInvoke.TaskDialogIndirect(config, out int pressed, out _, out BOOL ticked).ThrowOnFailure();
            return pressed == Go ? ticked : null;
        }
    }

    public static string StartMenuLink(Profile p) => Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Programs), p.DisplayName + ".lnk");
    public static string DesktopLink(Profile p) => Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.DesktopDirectory), p.DisplayName + ".lnk");

    // A shortcut to the installed app. The Start menu one is how Windows lists and finds Aetheris; its app ID is the
    // window's (AppWindow), so a taskbar pin and the open window are one button.
    public static unsafe void Shortcut(Profile p, string lnk)
    {
        var exe = Path.Combine(p.InstallDir, "Aetheris.exe");
        PInvoke.CoCreateInstance(typeof(ShellLink).GUID, null, CLSCTX.CLSCTX_INPROC_SERVER, out IShellLinkW* link).ThrowOnFailure();
        try
        {
            fixed (char* target = exe)
            {
                link->SetPath(target);
                link->SetIconLocation(target, 0);
            }
            fixed (char* args = p.Dev ? "--dev --show" : "--show")
                link->SetArguments(args);
            fixed (char* about = "Your files, on your own server")
                link->SetDescription(about);
            void* raw;
            var iid = typeof(IPropertyStore).GUID;
            link->QueryInterface(&iid, &raw).ThrowOnFailure();
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
            iid = typeof(IPersistFile).GUID;
            link->QueryInterface(&iid, &raw).ThrowOnFailure();
            var file = (IPersistFile*)raw;
            try
            {
                fixed (char* path = lnk)
                    file->Save(path, true);
            }
            finally
            {
                file->Release();
            }
        }
        finally
        {
            link->Release();
        }
    }

    // Every start of the installed copy: its own files for a new build, and the exe an update left behind goes.
    public static void Started(Profile p)
    {
        try
        {
            Unpack(p);
            File.Delete(Path.Combine(p.InstallDir, "Aetheris.old.exe"));
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            Log.Error($"setting up {p.InstallDir}: {e.Message}");
        }
    }

    // ui\ (the app window), package\ (Explorer's pane entry) and uninstall.ps1, next to the exe, once per build.
    static void Unpack(Profile p)
    {
        var marker = Path.Combine(p.InstallDir, "build.txt");
        var build = self.ManifestModule.ModuleVersionId.ToString();
        if (!Packed || (File.Exists(marker) && File.ReadAllText(marker) == build))
            return;
        var ui = Path.Combine(p.InstallDir, "ui");
        if (Directory.Exists(ui))
            Directory.Delete(ui, recursive: true); // the last build's hashed file names
        foreach (var name in self.GetManifestResourceNames().Where(n => n.StartsWith(@"ui\") || n.StartsWith(@"package\") || n == "uninstall.ps1"))
        {
            var dest = Path.Combine(p.InstallDir, name);
            Directory.CreateDirectory(Path.GetDirectoryName(dest)!);
            using var from = self.GetManifestResourceStream(name)!;
            using var to = File.Create(dest);
            from.CopyTo(to);
        }
        if (!p.Dev)
            RegisterPackage(p);
        File.WriteAllText(marker, build);
    }

    // Package identity gives Aetheris its own entry in Explorer's navigation pane (client-win/package). Windows takes
    // the unsigned package only in Developer Mode; without it the app runs as before, without the pane entry.
    // Registered when missing or when its version changed. Never removed here: removing the package can take its
    // sync root along, and with it every online-only placeholder.
    static void RegisterPackage(Profile p)
    {
        using (var k = Registry.LocalMachine.OpenSubKey(@"SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock"))
            if (k?.GetValue("AllowDevelopmentWithoutDevLicense") is not 1)
            {
                Log.Info("package identity: Windows takes it only in Developer Mode; running without the Explorer pane entry");
                return;
            }
        static string Quote(string s) => "'" + s.Replace("'", "''") + "'";
        var manifest = Quote(Path.Combine(p.InstallDir, "package", "AppxManifest.xml"));
        var script = $"$v = ([xml](Get-Content -LiteralPath {manifest})).Package.Identity.Version; $i = Get-AppxPackage -Name 'Aetheris.Drive'; " +
            $"if (-not $i -or $i.Version -ne $v) {{ Add-AppxPackage -Register {manifest} -ExternalLocation {Quote(p.InstallDir)} }}";
        using var ps = Process.Start(new ProcessStartInfo("powershell.exe") { ArgumentList = { "-NoProfile", "-NonInteractive", "-Command", script }, CreateNoWindow = true });
        if (ps == null || !ps.WaitForExit(60_000) || ps.ExitCode != 0)
            Log.Error("package identity: registering failed; running without the Explorer pane entry");
    }

    // The running app, found by its tray window: by name would also catch the server's aetheris.exe on a dev PC.
    public static unsafe void Quit(Profile p)
    {
        var tray = PInvoke.FindWindow("AetherisTray", p.DisplayName);
        if (tray == default)
            return;
        uint pid;
        PInvoke.GetWindowThreadProcessId(tray, &pid);
        PInvoke.PostMessage(tray, PInvoke.WM_CLOSE, 0, 0);
        try
        {
            using var app = Process.GetProcessById((int)pid);
            if (!app.WaitForExit(10_000))
            {
                app.Kill();
                app.WaitForExit();
            }
        }
        catch (ArgumentException) { } // already gone
    }
}
