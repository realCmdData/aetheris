namespace Aetheris;

enum Phase { Starting, NotPaired, Syncing, UpToDate, Offline, Paused, Failed }

// Runs the sync behind the tray and holds the status it shows. Changes and alerts may come from any thread.
static class Engine
{
    static Profile profile = null!;
    static CancellationTokenSource? follow;
    static bool connected;

    public static Phase Phase { get; private set; } = Phase.Starting;
    public static string Detail { get; private set; } = "";
    public static event Action? Changed;
    public static event Action<string>? Alert;

    public static long LastSync { get; private set; } // unix ms of the last time the NAS answered with nothing left to do

    public static void Set(Phase phase, string detail = "")
    {
        if (phase == Phase.UpToDate)
            LastSync = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
        if (Phase == phase && Detail == detail)
            return;
        (Phase, Detail) = (phase, detail);
        Changed?.Invoke();
    }

    public static void Refresh() => Changed?.Invoke();
    public static void Notify(string text)
    {
        Log.Info($"notification: {text}");
        Alert?.Invoke(text);
    }

    // Connects the cloud folders (once per run) and follows the NAS. Without a token it waits for pairing.
    public static async Task Start(Profile p)
    {
        profile = p;
        if (p.Token == null || p.Server == null)
        {
            Set(Phase.NotPaired);
            return;
        }
        try
        {
            SyncRoot.Use(new Nas(p));
            if (!connected)
            {
                await SyncRoot.Register(p, p.Main);
                SyncRoot.Track(p.Main);
                SyncRoot.Connect(p.Main);
                foreach (var f in p.Folders)
                    await Attach(p, f);
                connected = true;
                try { Drive.Map(p, Drive.Choice(p)); }
                catch (Exception e) { Log.Error($"drive letter: {e.Message}"); } // the folder works without it
                _ = Task.Run(AutoFreeLoop);
            }
            Resume();
        }
        catch (Exception e)
        {
            Log.Error($"start: {e}");
            Set(Phase.Failed, e.Message);
        }
    }

    // One folder from elsewhere on the PC; a folder that isn't here right now (an unplugged drive) waits for the next start.
    static async Task Attach(Profile p, Folder f)
    {
        if (!Directory.Exists(f.Local))
        {
            Log.Error($"{f.Local} isn't on this PC right now; syncing it again once it is (at the next start)");
            return;
        }
        try { await SyncRoot.Register(p, f); }
        catch (Exception e) { Log.Error($"registering {f.Local} again: {e.Message}"); } // the earlier registration still holds
        SyncRoot.Track(f);
        try { SyncRoot.Connect(f); }
        catch (Exception e)
        {
            SyncRoot.Untrack(f);
            Log.Error($"connecting {f.Local}: {e.Message}");
        }
    }

    static readonly SemaphoreSlim changing = new(1, 1);

    // Syncs a folder from elsewhere on this PC: the NAS keeps it under Computers/<this PC>/<its name>, and the web
    // shows it there. Two-way, and every file stays on this PC. null when added, otherwise what to tell the user.
    public static async Task<string?> AddFolder(Profile p, string path)
    {
        try { path = Path.TrimEndingDirectorySeparator(Path.GetFullPath(path)); }
        catch (Exception) { return "That isn't a folder path."; }
        if (Check(p, path) is { } problem)
            return problem;
        var name = Path.GetFileName(path);
        var taken = p.Folders.Select(x => x.Name).ToHashSet(StringComparer.OrdinalIgnoreCase);
        for (int n = 2; taken.Contains(name); n++)
            name = $"{Path.GetFileName(path)} ({n})";
        var f = new Folder(path, $"{Folder.Computers}/{Host}/{name}");
        await changing.WaitAsync();
        try
        {
            try { await SyncRoot.Register(p, f); }
            catch (Exception e)
            {
                Log.Error($"registering {path}: {e.Message}");
                return "Windows wouldn't let Aetheris sync this folder. Another app (OneDrive, say) may sync it already.";
            }
            if (connected)
            {
                SyncRoot.Track(f);
                try { SyncRoot.Connect(f); }
                catch (Exception e)
                {
                    SyncRoot.Untrack(f);
                    StorageUnregister(p, f); // nothing in it is a placeholder yet, so this changes no file
                    Log.Error($"connecting {path}: {e.Message}");
                    return "Windows wouldn't let Aetheris sync this folder.";
                }
            }
            p.ForgetSynced(f); // a fresh start: nothing here counts as deleted
            p.Folders = [.. p.Folders, f];
            if (connected)
                Restart();
        }
        finally
        {
            changing.Release();
        }
        Log.Info($"syncing {path} as {f.Nas}");
        Record("added", f.Nas);
        return null;
    }

    // The PC's name as Windows shows it; the web lists this PC's folders under it.
    public static string Host => System.Net.Dns.GetHostName();

    static string? Check(Profile p, string path)
    {
        static bool Inside(string a, string b) => a.Equals(b, StringComparison.OrdinalIgnoreCase) || a.StartsWith(b + "\\", StringComparison.OrdinalIgnoreCase);
        if (!Directory.Exists(path))
            return "That folder doesn't exist.";
        if (Path.GetPathRoot(path) == path + "\\" || Path.GetPathRoot(path) == path)
            return "Choose a folder, not a whole drive.";
        foreach (var f in p.All)
        {
            if (Inside(path, f.Local))
                return f.Backup ? $"That folder already syncs, as part of “{f.Name}”." : "That folder is inside your Aetheris folder, which syncs already.";
            if (Inside(f.Local, path))
                return f.Backup ? $"That folder holds “{f.Name}”, which already syncs. Stop syncing that one first." : "That folder holds your Aetheris folder. Choose one inside it instead.";
        }
        var system = new[] { Environment.SpecialFolder.Windows, Environment.SpecialFolder.ProgramFiles, Environment.SpecialFolder.ProgramFilesX86,
            Environment.SpecialFolder.CommonApplicationData, Environment.SpecialFolder.ApplicationData, Environment.SpecialFolder.LocalApplicationData };
        if (system.Select(Environment.GetFolderPath).Any(s => s != "" && (Inside(path, s) || Inside(s, path))))
            return "Windows and app folders can't sync.";
        var drive = new DriveInfo(path);
        if (drive.DriveType == DriveType.Network)
            return "Folders on network drives can't sync.";
        if (drive.DriveFormat != "NTFS")
            return $"Only folders on NTFS drives can sync (this drive is {drive.DriveFormat}).";
        return null;
    }

    // Stops syncing a folder from elsewhere on the PC. Its files stay here as normal files, and on the NAS under
    // Computers. gone: the NAS no longer has it, so files that are only there can't come here first.
    public static async Task<string?> RemoveFolder(Profile p, Folder f, bool gone = false)
    {
        await changing.WaitAsync();
        try
        {
            if (!p.Folders.Contains(f))
                return null; // already stopped
            bool running = connected && Phase is not (Phase.Paused or Phase.NotPaired or Phase.Failed);
            follow?.Cancel();
            // Windows deletes online-only files of a folder that stops syncing: they come here first (which needs the
            // folder connected, so with the app quit any such file stops it).
            int missing = Directory.Exists(f.Local) ? await Task.Run(() => Download(f)) : 0;
            if (missing > 0 && !gone)
            {
                if (running)
                    Resume();
                return $"{missing} file{(missing == 1 ? " is" : "s are")} still only on your NAS. Try again once {(missing == 1 ? "it has" : "they have")} downloaded.";
            }
            SyncRoot.Disconnect(f); // first: no callback may arrive for a folder that is no longer tracked
            SyncRoot.Untrack(f);
            StorageUnregister(p, f);
            p.Folders = [.. p.Folders.Where(x => x != f)];
            p.ForgetSynced(f);
            if (running)
                Resume();
        }
        finally
        {
            changing.Release();
        }
        Log.Info($"stopped syncing {f.Local}; its files stay here{(gone ? "" : $" and on the NAS as {f.Nas}")}");
        Record("stopped", f.Nas);
        return null;
    }

    static void StorageUnregister(Profile p, Folder f)
    {
        try { SyncRoot.Unregister(p, f); }
        catch (Exception e) { Log.Error($"unregistering {f.Local}: {e.Message}"); }
    }

    // Downloads what is still online-only; returns how many files couldn't come.
    static int Download(Folder f)
    {
        int missing = 0;
        foreach (var file in Local.Walk(f.Local, filesOnly: true))
        {
            try
            {
                if ((File.GetAttributes(file) & SyncRoot.Dehydrated) == 0)
                    continue;
                using var h = SyncRoot.Open(file, Remote.ShareAll);
                SyncRoot.Hydrate(h);
            }
            catch (Exception e)
            {
                Log.Error($"downloading {file}: {e.Message}");
                missing++;
            }
        }
        return missing;
    }

    // The NAS removed a folder from elsewhere on this PC (deleted or moved in the web's Computers): stop syncing it.
    public static void Dropped(Folder f) => _ = Task.Run(async () =>
    {
        if (profile.Folders.Contains(f) && await RemoveFolder(profile, f, gone: true) == null)
            Notify($"“{f.Name}” was removed from Computers on your NAS, so this PC stopped syncing it. Its files stay in {f.Local}.");
    });

    static void Restart()
    {
        if (connected && Phase is not (Phase.Paused or Phase.NotPaired or Phase.Failed))
            Resume();
    }

    // Connects this PC to a server: its tailnet address (as the web app shows it next to the pairing code) and, for
    // a PC without Tailscale, its address at home. null when paired, otherwise what to tell the user.
    public static async Task<string?> Pair(Profile p, string server, string? home, string code)
    {
        var url = p.Dev ? p.Server! : Address(server);
        if (url == null)
            return "Type your server’s address, as the web app shows it, e.g. aetheris.tail1234.ts.net.";
        string[] lan = home is { Length: > 0 } h ? [h.Trim()] : url == p.Server ? p.Lan : [];
        if (lan.Any(l => Nas.Home(l) == null))
            return "That address at home doesn’t look right. It’s like 192.168.1.20.";
        try
        {
            var claimed = await Nas.Claim(url, p.Dev ? null : lan, code);
            p.Token = claimed.Token;
            p.SaveConnection(claimed.Server is { } s ? new Uri(s) : url, claimed.Lan is { } l ? [l] : lan, claimed.Web ?? p.Web);
            return null;
        }
        catch (AuthException e) { return e.Message; }
        catch (HttpRequestException) { return $"Can’t reach {url.Host}. Away from home, turn on Tailscale; at home without it, add the server’s address at home."; }
        catch (Exception e) { return e.Message; }
    }

    // "aetheris.tail1234.ts.net" or "https://aetheris.tail1234.ts.net/" → https://aetheris.tail1234.ts.net
    static Uri? Address(string s)
    {
        s = s.Trim();
        if (!s.Contains("://"))
            s = "https://" + s;
        return Uri.TryCreate(s, UriKind.Absolute, out var u) && u.Scheme == Uri.UriSchemeHttps && u.Host.Contains('.')
            ? new Uri(u.GetLeftPart(UriPartial.Authority)) : null;
    }

    // Paused stops syncing both ways. What you do yourself still reaches the NAS: opening an online-only
    // file downloads it, and deletes and renames in the folder go through.
    public static void Pause()
    {
        follow?.Cancel();
        Set(Phase.Paused);
    }

    public static void Resume()
    {
        follow?.Cancel();
        follow = new CancellationTokenSource();
        Set(Phase.Syncing);
        var ct = follow.Token;
        _ = Task.Run(() => Follow(ct)); // off the tray thread, whoever calls
        _ = Task.Run(() => Uploads(ct));
    }

    static async Task Uploads(CancellationToken ct)
    {
        try
        {
            await Local.Run(SyncRoot.Client, SyncRoot.Folders, ct);
        }
        catch (OperationCanceledException) when (ct.IsCancellationRequested)
        {
        }
        catch (Exception e)
        {
            Log.Error($"uploads stopped: {e}");
        }
    }

    static async Task Follow(CancellationToken ct)
    {
        try
        {
            await Remote.Follow(SyncRoot.Client, profile, ct);
        }
        catch (OperationCanceledException) when (ct.IsCancellationRequested)
        {
        }
        catch (AuthException e)
        {
            Log.Error(e.Message);
            profile.ForgetToken();
            Set(Phase.NotPaired);
            Notify("This PC was signed out of your NAS. Open Aetheris to pair it again.");
        }
    }

    public record Transfer(string Dir, string Path, long Done, long Size);
    public record Event(string Kind, string Path, string? To, long Size, long At);
    static readonly List<Event> recent = [];

    // What the app window lists under Activity: the last 100 things that happened, newest first.
    public static void Record(string kind, string path, string? to = null, long size = 0)
    {
        lock (recent)
        {
            recent.Insert(0, new(kind, path, to, size, DateTimeOffset.UtcNow.ToUnixTimeMilliseconds()));
            if (recent.Count > 100)
                recent.RemoveAt(100);
        }
        Refresh();
    }

    public static Event[] Recent()
    {
        lock (recent)
            return recent.ToArray();
    }

    // Space on this PC: files that are here (bytes, count), online-only ones, and those kept here.
    public static (long Bytes, int Files, int Online, int Pinned) Storage(string root)
    {
        long bytes = 0;
        int files = 0, online = 0, pinned = 0;
        if (!Directory.Exists(root))
            return default;
        foreach (var f in new DirectoryInfo(root).EnumerateFiles("*", new EnumerationOptions { RecurseSubdirectories = true, AttributesToSkip = 0, IgnoreInaccessible = true }))
        {
            if ((f.Attributes & SyncRoot.Dehydrated) != 0)
                online++;
            else
            {
                files++;
                bytes += f.Length;
            }
            if ((f.Attributes & SyncRoot.Pinned) != 0)
                pinned++;
        }
        return (bytes, files, online, pinned);
    }

    // Makes every downloaded file online-only again, except those kept on this PC and those with unsent edits.
    // Free up space automatically: a few minutes after start, then every six hours, while the option is on.
    static async Task AutoFreeLoop()
    {
        await Task.Delay(TimeSpan.FromMinutes(3));
        while (true)
        {
            if (profile.AutoFreeDays is > 0 and var days && Phase is not (Phase.Paused or Phase.NotPaired))
                AutoFree(days);
            await Task.Delay(TimeSpan.FromHours(6));
        }
    }

    // Files not opened (nor changed) for `days` become online-only, in the Aetheris folder and in the folders from this
    // PC: the NAS keeps them, and opening one brings it back. Pinned files ("Always keep on this device"), files with
    // unsent edits and files in use stay. In a folder from this PC the file is marked unpinned too, which tells the
    // uploader not to fetch it back (Local.Pin).
    public static (long Bytes, int Files) AutoFree(int days)
    {
        var cutoff = DateTime.UtcNow.AddDays(-days);
        long freed = 0;
        int count = 0;
        foreach (var f in SyncRoot.Folders)
            foreach (var file in Local.Walk(f.Local, filesOnly: true))
            {
                try
                {
                    var info = new FileInfo(file);
                    if ((info.Attributes & (SyncRoot.Dehydrated | SyncRoot.Pinned)) != 0 || Local.Scratch(info.Name) || SyncRoot.Outside(file)
                        || (info.LastAccessTimeUtc > info.LastWriteTimeUtc ? info.LastAccessTimeUtc : info.LastWriteTimeUtc) > cutoff)
                        continue;
                    using var h = SyncRoot.Open(file, 0); // exclusive: skips files an app has open
                    if (SyncRoot.Info(h) is not { InSync: true })
                        continue;
                    SyncRoot.Dehydrate(h);
                    if (f.Backup)
                        SyncRoot.SetPin(h, Windows.Win32.Storage.CloudFilters.CF_PIN_STATE.CF_PIN_STATE_UNPINNED);
                    freed += info.Length;
                    count++;
                }
                catch (Exception e) when (e is IOException or UnauthorizedAccessException or System.Runtime.InteropServices.COMException) { }
            }
        if (count > 0)
            Record("free", $"{count} file{(count == 1 ? "" : "s")} not opened for {days} days", size: freed);
        Log.Info($"☁ automatically freed {freed:N0} bytes in {count} files not opened for {days} days");
        return (freed, count);
    }

    // Whether Windows records when files are opened (NtfsDisableLastAccessUpdate: low bit set = it doesn't).
    public static bool RecordsOpens()
    {
        using var k = Microsoft.Win32.Registry.LocalMachine.OpenSubKey(@"SYSTEM\CurrentControlSet\Control\FileSystem");
        return k?.GetValue("NtfsDisableLastAccessUpdate") is not int v || (v & 1) == 0;
    }

    public static long FreeUpSpace(string root)
    {
        long freed = 0;
        int count = 0;
        foreach (var f in new DirectoryInfo(root).EnumerateFiles("*", new EnumerationOptions { RecurseSubdirectories = true, AttributesToSkip = 0, IgnoreInaccessible = true }))
        {
            if ((f.Attributes & (SyncRoot.Dehydrated | SyncRoot.Pinned)) != 0)
                continue;
            try
            {
                using var h = SyncRoot.Open(f.FullName, 0); // exclusive: skips files an app has open
                if (SyncRoot.Info(h) is not { InSync: true })
                    continue;
                SyncRoot.Dehydrate(h);
                freed += f.Length;
                count++;
            }
            catch (Exception e) when (e is IOException or UnauthorizedAccessException or System.Runtime.InteropServices.COMException)
            {
            }
        }
        if (count > 0)
            Record("free", count == 1 ? "1 file" : $"{count} files", size: freed);
        Log.Info($"☁ freed {freed:N0} bytes in {count} files");
        return freed;
    }

    // This PC gives up its device token; the NAS forgets the session too.
    public static async Task SignOut(Profile p)
    {
        follow?.Cancel();
        try { await SyncRoot.Client.Logout(); }
        catch (Exception e) { Log.Error($"sign out: {e.Message}"); } // offline: the token is gone here anyway; revoke it in Settings
        p.ForgetToken();
        Set(Phase.NotPaired);
        Log.Info("signed out of the NAS");
    }

    public static void Stop()
    {
        follow?.Cancel();
        Drive.Unmap(); // the mapping would outlive the app, pointing at files that can't open
        if (connected)
            SyncRoot.Disconnect();
    }
}
