using System.Collections.Concurrent;
using System.Net;
using Windows.Win32.Storage.CloudFilters;

namespace Aetheris;

// PC → NAS: new and changed files go up, new folders are created. Deletes and renames take the other road,
// through the Cloud Files callbacks (SyncRoot.Forward). What needs uploading is read off each item itself:
// anything that is not an in-sync placeholder.
static class Local
{
    // Held while an upload is committed and its placeholder rewritten, and while NAS changes are applied,
    // so the NAS echoing our own upload back never looks like an edit made somewhere else.
    public static readonly SemaphoreSlim Gate = new(1, 1);
    public static string? Current { get; private set; } // the file going up, for the tray
    static long sent, sending;

    public static Engine.Transfer? Transfer() => Current is { } c ? new("up", c, Interlocked.Read(ref sent), sending) : null;

    // A long upload: one file after another (less than 5 s apart) for more than 5 s, e.g. a folder just added. The status
    // then says so, with an estimate, instead of flicking to "Up to date" between files. What is left comes from one
    // count of the unsent files (a walk of the folders, in the background); the time from the rate so far.
    public record Long(TimeSpan? Left, int Files, long Bytes, double Done);
    sealed record Backlog(long Bytes, int Files, long SentAt, long FilesAt, long CountedAt);
    const int Gap = 5000;
    static long sessionStart, lastActive, sessionBytes, sessionFiles;
    static volatile Backlog? backlog;
    static int counting;

    static void Began()
    {
        long now = Environment.TickCount64;
        if (now - Interlocked.Read(ref lastActive) > Gap)
        {
            (sessionStart, backlog) = (now, null);
            Interlocked.Exchange(ref sessionBytes, 0);
            Interlocked.Exchange(ref sessionFiles, 0);
        }
        Interlocked.Exchange(ref lastActive, now);
    }

    static void Ended(long bytes)
    {
        Interlocked.Add(ref sessionBytes, bytes);
        Interlocked.Increment(ref sessionFiles);
        Interlocked.Exchange(ref lastActive, Environment.TickCount64);
        _ = Task.Delay(Gap + 100).ContinueWith(_ => Engine.Refresh()); // the status changes once the gap has passed
    }

    public static Long? LongUpload()
    {
        long now = Environment.TickCount64;
        if ((Current == null && now - Interlocked.Read(ref lastActive) > Gap) || now - sessionStart < Gap)
            return null;
        var b = backlog;
        if (b == null || (b.Files - (Interlocked.Read(ref sessionFiles) - b.FilesAt) <= 0 && Current != null && now - b.CountedAt > 30_000))
        {
            if (Interlocked.Exchange(ref counting, 1) == 0)
                _ = Task.Run(Count); // more came than were counted: count again
            if (b == null)
                return new(null, 0, 0, 0);
        }
        long done = Interlocked.Read(ref sessionBytes) + (Current != null ? Interlocked.Read(ref sent) : 0);
        long left = Math.Max(0, b.Bytes - (done - b.SentAt));
        int files = (int)Math.Max(0, b.Files - (Interlocked.Read(ref sessionFiles) - b.FilesAt));
        if (files == 0 && Current == null)
            return null; // all sent: back to the usual status
        double rate = done / Math.Max(1.0, (now - sessionStart) / 1000.0); // bytes a second so far
        return new(rate > 0 ? TimeSpan.FromSeconds(left / rate) : null, files, left, done / (double)Math.Max(1, done + left));
    }

    static void Count()
    {
        try
        {
            long sentAt = Interlocked.Read(ref sessionBytes) + (Current != null ? Interlocked.Read(ref sent) : 0), filesAt = Interlocked.Read(ref sessionFiles);
            long bytes = 0;
            int files = 0;
            foreach (var f in SyncRoot.Folders)
                foreach (var file in Walk(f.Local, filesOnly: true))
                {
                    if (Scratch(Path.GetFileName(file)) || SyncRoot.Outside(file))
                        continue;
                    try
                    {
                        if ((File.GetAttributes(file) & SyncRoot.Dehydrated) != 0)
                            continue; // online-only: nothing here to send
                        using (var h = SyncRoot.Open(file, Remote.ShareAll))
                            if (SyncRoot.Info(h) is { InSync: true } || SyncRoot.Links(h) > 1)
                                continue;
                        bytes += new FileInfo(file).Length;
                        files++;
                    }
                    catch (Exception e) when (e is IOException or UnauthorizedAccessException or System.Runtime.InteropServices.COMException) { }
                }
            backlog = new(bytes, files, sentAt, filesAt, Environment.TickCount64);
            Log.Info($"long upload: {files:N0} files, {bytes:N0} bytes still to send");
        }
        catch (Exception e)
        {
            Log.Error($"counting what is left to upload: {e.Message}");
        }
        finally
        {
            Interlocked.Exchange(ref counting, 0);
        }
    }

    const int Quiet = 2000; // ms without changes before a file goes up: let apps finish writing
    static readonly ConcurrentDictionary<string, long> due = new(StringComparer.OrdinalIgnoreCase);

    public static async Task Run(Nas nas, Folder[] folders, CancellationToken ct)
    {
        var watchers = folders.Select(f =>
        {
            var watcher = new FileSystemWatcher(f.Local)
            {
                IncludeSubdirectories = true,
                InternalBufferSize = 64 * 1024,
                // Attributes: pinning and "Free up space" show up only as an attribute change.
                NotifyFilter = NotifyFilters.FileName | NotifyFilters.DirectoryName | NotifyFilters.LastWrite | NotifyFilters.Size | NotifyFilters.Attributes,
            };
            watcher.Created += (_, e) => Touch(e.FullPath);
            watcher.Renamed += (_, e) => Touch(e.FullPath);
            watcher.Changed += (_, e) => { if (!Directory.Exists(e.FullPath)) Touch(e.FullPath); }; // folders "change" whenever their contents do
            watcher.Error += (_, _) => Touch(f.Local); // events were lost: check everything
            watcher.EnableRaisingEvents = true;
            due[f.Local] = 0; // whatever changed while the app was off or paused
            return watcher;
        }).ToList();
        try
        {
            while (true)
            {
                await Task.Delay(500, ct);
                var now = Environment.TickCount64;
                foreach (var (path, when) in due)
                {
                    if (when > now || !due.TryRemove(new KeyValuePair<string, long>(path, when)))
                        continue; // not quiet yet, or touched again meanwhile
                    await Try(nas, path, ct);
                }
            }
        }
        catch (AuthException)
        {
            // The changes feed reports the sign-out.
        }
        finally
        {
            Current = null;
            watchers.ForEach(w => w.Dispose());
        }
    }

    // One item; a failure comes back on its own in 30 s, so one item that can't go never holds up the rest.
    static async Task Try(Nas nas, string path, CancellationToken ct)
    {
        try
        {
            await Sync(nas, path, ct);
        }
        catch (Exception e) when (e is not (OperationCanceledException or AuthException))
        {
            if (!Path.Exists(path) || (e is UnauthorizedAccessException && await GoneSoon(path, ct)))
                return; // deleted or moved meanwhile (a delete in progress refuses access): nothing left to send
            Log.Error($"sending {(SyncRoot.Of(path) != null ? SyncRoot.Rel(path) : path)}: {e.Message}");
            if (e is not HttpRequestException { StatusCode: HttpStatusCode.BadRequest }) // a name the NAS refuses stays refused
                due.TryAdd(path, Environment.TickCount64 + 30_000);
        }
    }

    static readonly ConcurrentDictionary<string, byte> hardLinked = new(StringComparer.OrdinalIgnoreCase); // logged once each

    static async Task<bool> GoneSoon(string path, CancellationToken ct)
    {
        await Task.Delay(1000, ct);
        return !Path.Exists(path);
    }

    // Also called for placeholders the NAS side creates: a pinned folder wants them downloaded.
    public static void Touch(string path) => due[path] = Environment.TickCount64 + Quiet;

    // Office lock files, Windows' folder settings and temp files stay on this PC.
    public static bool Scratch(string name) =>
        name.StartsWith("~$") || name.StartsWith(".~lock.") || name.EndsWith(".tmp", StringComparison.OrdinalIgnoreCase)
        || name.Equals("desktop.ini", StringComparison.OrdinalIgnoreCase) || name.Equals("Thumbs.db", StringComparison.OrdinalIgnoreCase);

    static async Task Sync(Nas nas, string local, CancellationToken ct)
    {
        bool isRoot = SyncRoot.Of(local) is { } f && f.Local.Equals(local, StringComparison.OrdinalIgnoreCase);
        if (!isRoot && (Scratch(Path.GetFileName(local)) || SyncRoot.Outside(local)))
            return;
        if (!isRoot && Foreign(local))
            return;
        if (Directory.Exists(local))
        {
            if (!isRoot)
            {
                using var h = SyncRoot.Open(local, Remote.ShareAll);
                if (SyncRoot.Info(h) == null)
                {
                    var rel = SyncRoot.Rel(local);
                    await EnsureParents(nas, rel, ct);
                    var e = await nas.Mkdir(rel, ct);
                    SyncRoot.Convert(h, e, inSync: true);
                    Log.Info($"↑ {rel}/");
                }
            }
            foreach (var child in Directory.EnumerateFileSystemEntries(local, "*", new EnumerationOptions { AttributesToSkip = 0, IgnoreInaccessible = true }))
                await Try(nas, child, ct);
            return;
        }
        if (File.Exists(local))
        {
            await Upload(nas, local, ct);
            Pin(local);
        }
    }

    // A link to elsewhere (a junction such as Documents' old "My Music", or a symbolic link): not followed, not sent.
    // Placeholders are reparse points too, so the link target tells them apart.
    public static bool Foreign(string local)
    {
        try
        {
            FileSystemInfo item = Directory.Exists(local) ? new DirectoryInfo(local) : new FileInfo(local);
            return (item.Attributes & FileAttributes.ReparsePoint) != 0 && item.LinkTarget != null;
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            return false;
        }
    }

    // Everything inside a folder (hidden items too), without following links to elsewhere.
    public static IEnumerable<string> Walk(string root, bool filesOnly = false) =>
        new System.IO.Enumeration.FileSystemEnumerable<string>(root, (ref System.IO.Enumeration.FileSystemEntry e) => e.ToFullPath(), Remote.Deep)
        {
            ShouldIncludePredicate = (ref System.IO.Enumeration.FileSystemEntry e) => !filesOnly || !e.IsDirectory,
            ShouldRecursePredicate = (ref System.IO.Enumeration.FileSystemEntry e) =>
                (e.Attributes & FileAttributes.ReparsePoint) == 0 || !Foreign(e.ToFullPath()),
        };

    // A folder of the NAS and its parents (folders from elsewhere on the PC sit under Computers/<this PC>).
    public static async Task EnsureFolder(Nas nas, string rel, CancellationToken ct)
    {
        await EnsureParents(nas, rel, ct);
        await nas.Mkdir(rel, ct);
    }

    static readonly SemaphoreSlim keeping = new(2); // pinned files download two at a time (each in up to 3 parts)
    static readonly ConcurrentDictionary<string, byte> fetching = new(StringComparer.OrdinalIgnoreCase);

    // "Always keep on this device" (on the file, or its folder for files that arrive later) downloads it.
    // "Free up space" drops the local bytes once the NAS has every edit, then forgets the request, so a later
    // open keeps the file here again (as OneDrive does).
    // Folders from elsewhere on the PC keep every file here, whatever its pin state.
    static void Pin(string local)
    {
        var a = File.GetAttributes(local);
        bool keep = SyncRoot.Of(local) is { Backup: true };
        if (keep && (a & SyncRoot.Unpinned) != 0)
            return; // freed by the automatic option (Engine.AutoFree): online-only until it is opened again
        if (!keep && (a & SyncRoot.Unpinned) != 0)
        {
            using var h = SyncRoot.Open(local, (a & SyncRoot.Dehydrated) != 0 ? Remote.ShareAll : 0); // exclusive to dehydrate
            if (SyncRoot.Info(h) is not { InSync: true })
                return; // unsent edits stay until they are on the NAS; the upload's own event brings us back
            if ((a & SyncRoot.Dehydrated) == 0)
            {
                SyncRoot.Dehydrate(h);
                Log.Info($"☁ {SyncRoot.Rel(local)} (space freed)");
                Engine.Record("free", SyncRoot.Rel(local), size: new FileInfo(local).Length);
            }
            SyncRoot.SetPin(h, CF_PIN_STATE.CF_PIN_STATE_UNSPECIFIED);
            return;
        }
        bool pinned = keep || (a & SyncRoot.Pinned) != 0;
        bool folderPinned = !pinned && (File.GetAttributes(Path.GetDirectoryName(local)!) & SyncRoot.Pinned) != 0;
        if ((a & SyncRoot.Dehydrated) == 0 || !(pinned || folderPinned) || !fetching.TryAdd(local, 0))
            return;
        _ = Task.Run(async () =>
        {
            await keeping.WaitAsync();
            try
            {
                using var h = SyncRoot.Open(local, Remote.ShareAll);
                if (folderPinned)
                    SyncRoot.SetPin(h, CF_PIN_STATE.CF_PIN_STATE_PINNED);
                SyncRoot.Hydrate(h);
                Log.Info($"📌 {SyncRoot.Rel(local)} (kept on this PC)");
                Engine.Record("keep", SyncRoot.Rel(local));
            }
            catch (Exception e)
            {
                Log.Error($"keeping {SyncRoot.Rel(local)} on this PC: {e.Message}");
            }
            finally
            {
                keeping.Release();
                fetching.TryRemove(local, out _);
            }
        });
    }

    static async Task EnsureParents(Nas nas, string rel, CancellationToken ct)
    {
        for (int i = rel.IndexOf('/'); i >= 0; i = rel.IndexOf('/', i + 1))
            await nas.Mkdir(rel[..i], ct); // idempotent
    }

    static async Task Upload(Nas nas, string local, CancellationToken ct)
    {
        Placeholder? before;
        using (var h = SyncRoot.Open(local, Remote.ShareAll))
        {
            before = SyncRoot.Info(h);
            if (before is { InSync: true })
                return;
            if (before == null && SyncRoot.Links(h) > 1)
            {
                // It could go up but never be marked as sent, so it would go up again and again (and once found on
                // the NAS, as a conflict copy). Such files stay on this PC only.
                if (hardLinked.TryAdd(local, 0))
                    Log.Error($"{SyncRoot.Rel(local)}: not synced: it has hard links, which Windows' cloud files can't hold");
                return;
            }
            if (before != null && SyncRoot.WasInSync(local))
            {
                SyncRoot.MarkInSync(h); // only renamed or moved since it was last in sync
                return;
            }
            if (before != null && (File.GetAttributes(local) & SyncRoot.Dehydrated) != 0)
            {
                SyncRoot.MarkInSync(h); // not even on this PC, so not changed here: only its times were touched
                return;
            }
        }
        var rel = SyncRoot.Rel(local);
        var file = new FileInfo(local);
        long size = file.Length;
        var written = file.LastWriteTimeUtc;
        long created = new DateTimeOffset(file.CreationTimeUtc).ToUnixTimeMilliseconds();
        string id;
        Began();
        (Current, sent, sending) = (rel, 0, size);
        Engine.Refresh();
        try
        {
            await using var data = new FileStream(local, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete,
                1 << 16, FileOptions.SequentialScan | FileOptions.Asynchronous);
            id = await nas.Upload(data, size, ct, n => { Interlocked.Exchange(ref sent, n); Engine.Refresh(); });
        }
        finally
        {
            Current = null;
            Engine.Refresh();
        }

        long mtime = new DateTimeOffset(written).ToUnixTimeMilliseconds();
        await Gate.WaitAsync(ct);
        try
        {
            string target = rel;
            long? swapped = SyncRoot.Replacing(local);
            long baseRev = before?.Rev ?? swapped ?? 0; // a safe save edits the file it replaces
            bool plain = before == null && swapped == null; // a plain file: new here, or saved over the placeholder through a temp file
            Entry? theirs = null; // the NAS version, when both sides changed
            int copies = 0;
            bool parents = false;
            Entry e;
            for (int attempt = 1; ; attempt++)
            {
                try
                {
                    e = await nas.Commit(id, target, baseRev, mtime, created, ct);
                    break;
                }
                catch (ConflictException c) when (attempt < 6)
                {
                    if (target == rel && before == null && c.Current is { } there && Remote.Same(local, there))
                    {
                        e = there; // this very file is on the NAS already (sent twice, or sent before it could be marked)
                        break;
                    }
                    if (target == rel && c.Current is { } cur && (cur.Hash == before?.Hash || (plain && Remote.Had(rel, cur.Rev))))
                        baseRev = cur.Rev; // only moved or touched on the NAS, or the version this PC had and an app saved over: still the one we edited
                    else if (target == rel && c.Current == null)
                        baseRev = 0; // deleted on the NAS meanwhile: the edit wins and brings it back
                    else
                    {
                        theirs ??= c.Current;
                        target = ConflictName(rel, ++copies);
                        baseRev = 0;
                    }
                }
                catch (HttpRequestException x) when (x.StatusCode == HttpStatusCode.Conflict && !parents && attempt < 6)
                {
                    parents = true;
                    await EnsureParents(nas, target, ct); // its folder isn't on the NAS yet
                }
            }

            // Edited again while it went up? Then it stays unsynced and goes up once more.
            file.Refresh();
            bool same = file.Exists && file.Length == size && file.LastWriteTimeUtc == written;
            var path = local;
            if (target != rel)
            {
                // Changed on both sides. This PC's version lives on under the conflict name, here and on the NAS;
                // the original name gets the NAS version back.
                path = SyncRoot.LocalPath(target)!;
                File.Move(local, path);
                if (theirs != null)
                    SyncRoot.CreatePlaceholders([theirs], out _);
                Engine.Notify($"“{Path.GetFileName(rel)}” was changed on this PC and on your NAS. Your version is saved as “{Path.GetFileName(target)}”.");
                Engine.Record("conflict", rel, target);
            }
            using (var h = SyncRoot.Open(path, Remote.ShareAll))
            {
                if (before == null)
                    SyncRoot.Convert(h, e, same);
                else
                    SyncRoot.SetIdentity(h, e, same);
            }
            SyncRoot.Replaced(local);
            Remote.Settled(rel);
            Log.Info($"↑ {target} ({size:N0} bytes{(target != rel ? ", conflict copy" : "")})");
            Ended(size);
            if (target == rel)
                Engine.Record("up", rel, size: size);
        }
        finally
        {
            Gate.Release();
        }
    }

    // "a.txt" → "a (conflict DESKTOP-01 2026-11-06 16.40).txt", then "… 2)", "… 3)" if that exists too.
    static string ConflictName(string rel, int n)
    {
        int slash = rel.LastIndexOf('/') + 1;
        var name = rel[slash..];
        var ext = Path.GetExtension(name);
        return $"{rel[..slash]}{name[..^ext.Length]} (conflict {Environment.MachineName} {DateTime.Now:yyyy-MM-dd HH.mm}{(n > 1 ? $" {n}" : "")}){ext}";
    }
}
