using System.ComponentModel;
using Microsoft.Win32;
using Windows.Win32;
using Windows.Win32.Storage.FileSystem;
using Windows.Win32.UI.Shell;

namespace Aetheris;

// Optional (off unless chosen in the app): the Aetheris folder also as a drive letter, like Google Drive's G:.
// Explorer shows cloud status, thumbnails and "Always keep on this device" only on the sync root's real path, so
// the way in is the Aetheris entry in Explorer's navigation pane (as with OneDrive); the drive is a shortcut.
// It is a per-user drive mapping (what `subst` does) that lives while the app runs, which is also when its files
// can open. Name and icon are per-user Explorer settings. No admin rights anywhere.
static class Drive
{
    const string Labels = @"Software\Classes\Applications\Explorer.exe\Drives\";
    static char? mapped;
    static string target = "";

    public static char? Letter => mapped;

    // Letters nobody uses, plus ours.
    public static List<char> Free()
    {
        uint used = PInvoke.GetLogicalDrives();
        return Enumerable.Range('A', 26).Select(i => (char)i)
            .Where(c => c == mapped || (used & (1u << (c - 'A'))) == 0 && c is not ('B'))
            .ToList();
    }

    // The saved choice; off until you pick a letter.
    public static char? Choice(Profile p)
    {
        if (p.Drive is { } saved)
            return saved == "off" ? null : saved[0];
        p.Drive = "off";
        return null;
    }

    public static void Map(Profile p, char? letter)
    {
        Unmap();
        p.Drive = letter?.ToString() ?? "off";
        if (letter is not { } c)
            return;
        if (Target(c) is { } t)
        {
            // A second mapping would silently cover whatever has the letter now; only a leftover of ours is replaced.
            if (!t.Equals(@"\??\" + p.Root, StringComparison.OrdinalIgnoreCase))
            {
                Log.Error($"drive {c}: is taken ({t}); choose another letter in Aetheris");
                return;
            }
            PInvoke.DefineDosDevice(DEFINE_DOS_DEVICE_FLAGS.DDD_REMOVE_DEFINITION | DEFINE_DOS_DEVICE_FLAGS.DDD_EXACT_MATCH_ON_REMOVE, $"{c}:", p.Root);
        }
        if (!PInvoke.DefineDosDevice(0, $"{c}:", p.Root))
            throw new Win32Exception();
        (mapped, target) = (c, p.Root);
        using (var k = Registry.CurrentUser.CreateSubKey(Labels + c + @"\DefaultLabel"))
            k.SetValue("", p.DisplayName);
        using (var k = Registry.CurrentUser.CreateSubKey(Labels + c + @"\DefaultIcon"))
            k.SetValue("", Environment.ProcessPath + ",0");
        Notify(SHCNE_ID.SHCNE_DRIVEADD, c);
        Log.Info($"{p.Root} is also drive {c}:");
    }

    public static void Unmap()
    {
        if (mapped is not { } c)
            return;
        PInvoke.DefineDosDevice(DEFINE_DOS_DEVICE_FLAGS.DDD_REMOVE_DEFINITION | DEFINE_DOS_DEVICE_FLAGS.DDD_EXACT_MATCH_ON_REMOVE, $"{c}:", target);
        Registry.CurrentUser.DeleteSubKeyTree(Labels + c, throwOnMissingSubKey: false);
        Notify(SHCNE_ID.SHCNE_DRIVEREMOVED, c);
        mapped = null;
    }

    // What the letter points at now (a mapping's target is "\??\C:\..."), or null when it is free.
    static string? Target(char c)
    {
        Span<char> buf = stackalloc char[1024];
        uint n = PInvoke.QueryDosDevice($"{c}:", buf);
        return n == 0 ? null : buf[..(int)n].ToString().Split('\0')[0];
    }

    static unsafe void Notify(SHCNE_ID e, char c)
    {
        fixed (char* path = $"{c}:\\")
            PInvoke.SHChangeNotify(e, SHCNF_FLAGS.SHCNF_PATHW, path, null);
    }
}
