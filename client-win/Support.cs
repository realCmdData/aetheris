using System.Diagnostics;
using System.IO.Compression;
using System.Text;

namespace Aetheris;

// "Export support log…" in the app window: one zip with what it takes to diagnose a problem. Live state and recent
// activity, the profile's settings files, and the newest part of the log and the previous run's log.
// Never token.bin (the device's sign-in) or anything else outside the *.txt settings.
static class Support
{
    const long Tail = 8 << 20; // the newest 8 MB of each log: between starts a log can grow to tens of MB

    public static string Export(Profile profile)
    {
        var dir = Path.Combine(profile.Dir, "support");
        Directory.CreateDirectory(dir);
        var zip = Path.Combine(dir, $"aetheris-support-{DateTime.Now:yyyy-MM-dd-HHmmss}.zip");
        using (var archive = ZipFile.Open(zip, ZipArchiveMode.Create))
        {
            Add(archive, "summary.txt", Summary(profile));
            Add(archive, "activity.txt", string.Join('\n', Engine.Recent().Select(e =>
                $"{DateTimeOffset.FromUnixTimeMilliseconds(e.At).LocalDateTime:yyyy-MM-dd HH:mm:ss} {e.Kind} {e.Path}{(e.To != null ? $" → {e.To}" : "")}{(e.Size > 0 ? $" ({e.Size:N0} bytes)" : "")}")));
            Copy(archive, profile.LogFile, "aetheris.log", Tail);
            Copy(archive, profile.LogFile + ".old", "aetheris.log.old", Tail);
            foreach (var f in Directory.EnumerateFiles(profile.Dir, "*.txt")) // folders, sync points, server address, options
                Copy(archive, f, "settings/" + Path.GetFileName(f), long.MaxValue);
        }
        Log.Info($"support bundle: {zip}");
        return zip;
    }

    static string Summary(Profile profile)
    {
        var me = Process.GetCurrentProcess();
        var s = new StringBuilder();
        s.AppendLine($"Aetheris {Setup.Shown}{(profile.Dev ? " (dev)" : "")}, exported {DateTime.Now:yyyy-MM-dd HH:mm:ss zzz}");
        s.AppendLine($"Program: {Environment.ProcessPath}, running since {me.StartTime:yyyy-MM-dd HH:mm:ss}, {me.WorkingSet64 >> 20} MB in memory");
        s.AppendLine($"Windows {Environment.OSVersion.Version}, .NET {Environment.Version}, {Environment.MachineName}");
        s.AppendLine($"Status: {Tray.Status()} ({Engine.Phase}){(Engine.Detail != "" ? $": {Engine.Detail}" : "")}");
        s.AppendLine($"Last synced: {(Engine.LastSync > 0 ? DateTimeOffset.FromUnixTimeMilliseconds(Engine.LastSync).LocalDateTime.ToString("yyyy-MM-dd HH:mm:ss") : "not yet")}, route: {Nas.Route ?? "none yet"}");
        s.AppendLine($"Server: {profile.Server?.Host ?? "not paired"}, at home: {string.Join(", ", profile.Lan)}, web: {profile.Web}");
        s.AppendLine($"Uploading: {Local.Current ?? "nothing"}{(Local.LongUpload() is { } big ? $", long upload: {big.Files:N0} files, {big.Bytes:N0} bytes left" : "")}");
        foreach (var t in SyncRoot.Transfers())
            s.AppendLine($"Downloading: {t.Path} ({t.Done:N0} of {t.Size:N0} bytes)");
        s.AppendLine($"Aetheris folder: {profile.Root}");
        foreach (var f in profile.Folders)
            s.AppendLine($"Folder from this PC: {f.Local} → {f.Nas}{(SyncRoot.Folders.Contains(f) ? "" : " (not syncing right now)")}");
        return s.ToString();
    }

    static void Add(ZipArchive zip, string name, string text)
    {
        using var w = new StreamWriter(zip.CreateEntry(name).Open(), new UTF8Encoding(false));
        w.Write(text);
    }

    // Shared reads: the log is open for writing the whole time.
    static void Copy(ZipArchive zip, string file, string name, long tail)
    {
        if (!File.Exists(file))
            return;
        using var src = new FileStream(file, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete);
        src.Seek(Math.Max(0, src.Length - tail), SeekOrigin.Begin); // may start mid-line
        using var dst = zip.CreateEntry(name).Open();
        src.CopyTo(dst);
    }
}
