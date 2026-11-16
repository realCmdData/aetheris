using System.Diagnostics;
using Microsoft.Win32;
using Windows.Win32;
using Windows.Win32.Foundation;
using Windows.Win32.UI.WindowsAndMessaging;

namespace Aetheris;

// Takes Aetheris off this PC (Settings > Apps > Installed apps runs uninstall.ps1, which runs this first). Nothing
// is lost: the NAS keeps everything, and what is on this PC stays in the folder as plain files. Order matters: the
// folder is unregistered here, before uninstall.ps1 removes the package (that would unregister it too) and the
// program files, from outside the package.
static class Uninstall
{
    const string Key = @"Software\Microsoft\Windows\CurrentVersion\Uninstall\Aetheris";

    // The entry in Installed apps, rewritten at every start (like autostart) so it follows the exe.
    public static void Advertise()
    {
        var exe = Environment.ProcessPath!;
        using var k = Registry.CurrentUser.CreateSubKey(Key);
        k.SetValue("DisplayName", "Aetheris");
        k.SetValue("DisplayVersion", Setup.Shown);
        k.SetValue("DisplayIcon", exe + ",0");
        k.SetValue("Publisher", "Aetheris");
        k.SetValue("InstallLocation", Path.GetDirectoryName(exe)!);
        var script = $"powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File \"{Path.Combine(Path.GetDirectoryName(exe)!, "uninstall.ps1")}\"";
        k.SetValue("UninstallString", script);
        k.SetValue("QuietUninstallString", script + " -Quiet");
        k.SetValue("NoModify", 1, RegistryValueKind.DWord);
        k.SetValue("NoRepair", 1, RegistryValueKind.DWord);
    }

    public static async Task<int> Run(Profile p, bool quiet)
    {
        const MESSAGEBOX_STYLE ask = MESSAGEBOX_STYLE.MB_YESNO | MESSAGEBOX_STYLE.MB_ICONQUESTION | MESSAGEBOX_STYLE.MB_DEFBUTTON2;
        if (!quiet && PInvoke.MessageBox(default,
                $"Remove {p.DisplayName} from this PC?\n\nYour files on the NAS stay as they are. Files downloaded to this PC stay in {p.Root} as normal files; online-only ones disappear from this PC.",
                $"Uninstall {p.DisplayName}", ask) != MESSAGEBOX_RESULT.IDYES)
            return 1;

        Setup.Quit(p);
        int unsent = Unsent(p.Root);
        await SignOut(p);
        // Online-only placeholders go; downloaded files become plain files. Folders from elsewhere on the PC keep
        // all their files here anyway, and stay untouched otherwise.
        foreach (var f in p.All)
        {
            try { SyncRoot.Unregister(p, f); }
            catch (Exception e) { Log.Error($"unregister {f.Local}: {e.Message}"); }
        }
        p.Autostart = false;
        File.Delete(Setup.StartMenuLink(p));
        File.Delete(Setup.DesktopLink(p));
        try { Plain(p.Root); }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException) { Log.Error($"tidying {p.Root}: {e.Message}"); }
        if (!p.Dev)
            Registry.CurrentUser.DeleteSubKeyTree(Key, throwOnMissingSubKey: false);
        int kept = Directory.Exists(p.Root) ? Directory.EnumerateFiles(p.Root, "*", SearchOption.AllDirectories).Count() : 0;
        Log.Info($"uninstalled: {kept} files stay in {p.Root} ({unsent} of them not on the NAS)");
        Log.Close();
        try { Directory.Delete(p.Dir, recursive: true); } // the token (already revoked) and the log
        catch (Exception e) when (e is IOException or UnauthorizedAccessException) { }

        if (!quiet)
            PInvoke.MessageBox(default, kept == 0 ? $"{p.DisplayName} was removed."
                : $"{p.DisplayName} was removed. {kept} file{(kept == 1 ? "" : "s")} stay in {p.Root}{(unsent > 0 ? $", {unsent} of them not on your NAS" : "")}.",
                p.DisplayName, MESSAGEBOX_STYLE.MB_ICONINFORMATION);
        return 0;
    }

    // Files the NAS doesn't have yet: local edits and new files. They stay; the final message says so.
    static int Unsent(string root)
    {
        if (!Directory.Exists(root))
            return 0;
        int n = 0;
        foreach (var f in Directory.EnumerateFiles(root, "*", new EnumerationOptions { RecurseSubdirectories = true, AttributesToSkip = 0, IgnoreInaccessible = true }))
        {
            if (Local.Scratch(Path.GetFileName(f)))
                continue;
            try
            {
                using var h = SyncRoot.Open(f, Remote.ShareAll);
                if (SyncRoot.Info(h) is not { InSync: true })
                    n++;
            }
            catch (Exception e) when (e is IOException or UnauthorizedAccessException) { }
        }
        return n;
    }

    // Best effort: the device session ends on the NAS too, so no valid token outlives the app.
    static async Task SignOut(Profile p)
    {
        if (p.Token == null)
            return;
        try
        {
            var logout = new Nas(p).Logout();
            if (await Task.WhenAny(logout, Task.Delay(8000)) != logout)
                Log.Error("sign out: the NAS didn't answer; revoke this PC in Settings on the web");
            else
                await logout;
        }
        catch (Exception e)
        {
            Log.Error($"sign out: {e.Message}; revoke this PC in Settings on the web");
        }
    }

    // A normal folder again: no logo, no read-only cue, and empty folders (left by online-only files) removed.
    static void Plain(string root)
    {
        if (!Directory.Exists(root))
            return;
        var ini = Path.Combine(root, "desktop.ini");
        if (File.Exists(ini))
        {
            File.SetAttributes(ini, System.IO.FileAttributes.Normal);
            File.Delete(ini);
        }
        var dir = new DirectoryInfo(root);
        dir.Attributes &= ~System.IO.FileAttributes.ReadOnly;
        foreach (var d in Directory.EnumerateDirectories(root, "*", SearchOption.AllDirectories).OrderByDescending(d => d.Length))
            if (!Directory.EnumerateFileSystemEntries(d).Any())
                Directory.Delete(d);
        if (!Directory.EnumerateFileSystemEntries(root).Any())
            Directory.Delete(root);
    }
}
