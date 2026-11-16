// Aetheris for Windows: the NAS as a cloud folder in Explorer (Files On-Demand), plus folders from elsewhere on
// the PC that sync to the NAS's Computers section.
//
//   Aetheris [--dev] [--show]           the tray app: first-time setup (pairing, folders), keeps the folders in sync.
//                                       The released exe started from outside the install folder installs itself first
//                                       (Windows' setup dialog; --quiet installs without asking).
//   Aetheris pair [<server>] <code> [--dev]  pair from a terminal instead of the app window
//   Aetheris folders | add <path> | remove <path> [--dev]
//                                       list, add or stop syncing folders from elsewhere on the PC (with the app quit)
//   Aetheris uninstall [--quiet] [--dev] the app's part of removing Aetheris (uninstall.ps1 runs it; downloaded files stay)
//   Aetheris unregister [--dev]         remove only the Aetheris folder's registration (development)
//
// --dev talks to the loopback dev server (http://127.0.0.1:8080) and uses the folder "Aetheris Dev".
// --after <pid> (from an update) waits for that process, the version being replaced, to quit first.
using System.Diagnostics;
using Aetheris;
using Windows.Win32;

var profile = new Profile(args.Contains("--dev"));
var words = args.Where(a => a is not ("--dev" or "--show" or "--quiet")).ToArray();
Log.Open(profile.LogFile);
if (words is ["--after", var pid])
{
    try
    {
        using var old = Process.GetProcessById(int.Parse(pid));
        old.WaitForExit(60_000);
    }
    catch (ArgumentException) { } // already gone
    Log.Info($"updated to {Setup.Shown}");
    words = [];
}
if (words.Length == 0)
{
    if (Setup.Packed && !Setup.Installed(profile))
    {
        int installed = 1;
        var setup = new Thread(() => installed = Setup.Install(profile, quiet: args.Contains("--quiet"))); // COM and the dialog want STA
        setup.SetApartmentState(ApartmentState.STA);
        setup.Start();
        setup.Join();
        return installed;
    }
    if (Setup.Installed(profile))
        Setup.Started(profile);
    int code = 0;
    var ui = new Thread(() => code = Tray.Run(profile, show: args.Contains("--show"))); // WebView2 wants a single-threaded apartment with a message loop
    ui.SetApartmentState(ApartmentState.STA);
    ui.Start();
    ui.Join();
    return code;
}

// A GUI exe has no console of its own: print into the terminal that started it.
if (PInvoke.AttachConsole(PInvoke.ATTACH_PARENT_PROCESS))
    Console.OutputEncoding = System.Text.Encoding.UTF8;
try
{
    switch (words[0])
    {
        case "pair" when words.Length is 2 or 3:
            if (await Engine.Pair(profile, words.Length == 3 ? words[1] : profile.Server?.Host ?? "", null, words[^1]) is { } failed)
            {
                Log.Error(failed);
                return 1;
            }
            Log.Info($"Paired with {profile.Server!.Host}. Start Aetheris{(profile.Dev ? " --dev" : "")} to sync.");
            return 0;
        case "uninstall":
            return await Uninstall.Run(profile, quiet: args.Contains("--quiet"));
        case "unregister":
            SyncRoot.Unregister(profile, profile.Main);
            profile.Autostart = false;
            profile.Drive = null;
            profile.ForgetSynced(profile.Main); // the folder's state no longer says anything about the NAS
            profile.RegisteredAs = null;
            Log.Info($"{profile.Root} is no longer a cloud folder.");
            return 0;
        case "folders":
            foreach (var f in profile.Folders)
                Console.WriteLine($"{f.Local}  →  {f.Nas}");
            return 0;
        case "add" when words.Length == 2:
            if (await Engine.AddFolder(profile, words[1]) is { } refused)
            {
                Log.Error(refused);
                return 1;
            }
            Log.Info($"{words[1]} syncs from the next start of Aetheris.");
            return 0;
        case "remove" when words.Length == 2:
            var folder = profile.Folders.FirstOrDefault(f => f.Local.Equals(Path.TrimEndingDirectorySeparator(Path.GetFullPath(words[1])), StringComparison.OrdinalIgnoreCase));
            if (folder == null || await Engine.RemoveFolder(profile, folder) is { } kept)
            {
                Log.Error(folder == null ? $"{words[1]} isn't one of the synced folders" : "its online-only files can't download with the app quit: stop syncing it in the app instead");
                return 1;
            }
            return 0;
        default:
            Console.Error.WriteLine("usage: Aetheris [pair [<server>] <code> | folders | add <path> | remove <path> | uninstall [--quiet] | unregister] [--dev]");
            return 2;
    }
}
catch (AuthException e)
{
    Log.Error(e.Message);
    return 1;
}
catch (HttpRequestException e)
{
    Log.Error($"couldn't reach {profile.Server?.Host ?? "the server"}: {e.Message}");
    return 1;
}

static class Log
{
    static StreamWriter? file;
    static readonly Lock gate = new();

    // The previous log is kept as .old once it passes 1 MB. Shared, so `pair` can log while the tray runs.
    public static void Open(string path)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        try
        {
            if (new FileInfo(path) is { Exists: true, Length: > 1 << 20 })
                File.Move(path, path + ".old", overwrite: true);
        }
        catch (IOException) { } // the other instance has it open; rotate next time
        file = new StreamWriter(new FileStream(path, FileMode.Append, FileAccess.Write, FileShare.ReadWrite | FileShare.Delete)) { AutoFlush = true };
    }

    public static void Close()
    {
        lock (gate)
        {
            file?.Dispose();
            file = null;
        }
    }

    public static void Info(string s) => Write(Console.Out, s);
    public static void Error(string s) => Write(Console.Error, "⚠ " + s);

    static void Write(TextWriter console, string s)
    {
        var line = $"{DateTime.Now:yyyy-MM-dd HH:mm:ss} {s}";
        lock (gate)
        {
            console.WriteLine(line);
            file?.WriteLine(line);
        }
    }
}
