using Windows.Win32.Storage.CloudFilters;
using Windows.Win32.Storage.FileSystem;

namespace Aetheris;

record Change(long Seq, string Op, string Path, string? From, Entry? Entry);

// NAS → PC: keep the placeholders matching the NAS. Only in-sync placeholders are ever replaced or deleted;
// local edits and new local files stay untouched (sending them up is the other direction's job).
static class Remote
{
    public const FILE_SHARE_MODE ShareAll = FILE_SHARE_MODE.FILE_SHARE_READ | FILE_SHARE_MODE.FILE_SHARE_WRITE | FILE_SHARE_MODE.FILE_SHARE_DELETE;

    // What this PC has seen of the NAS: every change up to `applied`, except the NAS versions it couldn't take
    // because a plain file or an unsent edit stood in their place (`skipped`).
    static long applied;
    static readonly System.Collections.Concurrent.ConcurrentDictionary<string, byte> skipped = new(StringComparer.OrdinalIgnoreCase);

    // Whether this PC had this NAS version of the file. An app that saves through a temp file renamed over the
    // original (Claude Code, many editors and slicers) leaves a plain file where the placeholder was, so the
    // uploader can't read the version it replaced: if it is the NAS's current one, the save is an edit of it.
    public static bool Had(string path, long rev) => rev <= Interlocked.Read(ref applied) && !skipped.ContainsKey(path);

    // The file under this name now is one the NAS has (an upload, or the NAS copy put back beside a conflict copy).
    public static void Settled(string path) => skipped.TryRemove(path, out _);

    // The protocol's client loop: the whole tree once, then the changes feed forever. A 410 (index
    // rebuilt) or a restart starts over from the tree. Offline, it retries with backoff up to a minute.
    public static async Task Follow(Nas nas, Profile profile, CancellationToken ct)
    {
        string? epoch = null;
        long cursor = 0;
        int backoff = 1;
        // A paused loop must not overwrite "Paused" with a late "Up to date".
        void Status(Phase phase, string detail = "") { if (!ct.IsCancellationRequested) Engine.Set(phase, detail); }
        while (true)
        {
            try
            {
                if (epoch == null)
                {
                    Status(Phase.Syncing);
                    var started = Environment.TickCount64;
                    // Under the gate from listing to reconcile: a file uploaded in between would look
                    // deleted on the NAS and be removed here.
                    await Local.Gate.WaitAsync(ct);
                    int created = 0, updated = 0, removed = 0;
                    List<Entry> entries;
                    try
                    {
                        (entries, var fresh, cursor) = await nas.Tree(ct);
                        foreach (var f in SyncRoot.Folders)
                        {
                            var (c, u, r) = await Reconcile(nas, profile, f, entries, fresh, ct);
                            (created, updated, removed) = (created + c, updated + u, removed + r);
                            profile.SetSynced(f, fresh, cursor);
                        }
                        epoch = fresh; // only now: a reconcile cut short starts over from the tree
                        Interlocked.Exchange(ref applied, cursor);
                    }
                    finally
                    {
                        Local.Gate.Release();
                    }
                    foreach (var f in SyncRoot.Folders)
                    {
                        Local.Touch(f.Local); // a fresh look at everything: pins, and edits made meanwhile
                        if (f.Backup)
                            _ = FolderIcon.Send(nas, f, ct); // the web shows it with the folder's own icon
                    }
                    Log.Info($"{entries.Count:N0} items on the NAS: {created:N0} new, {updated:N0} updated, {removed:N0} removed ({Environment.TickCount64 - started} ms).");
                    Status(Phase.UpToDate);
                }
                // Right after an error, ask without waiting: the status turns back to "Up to date" at once.
                var page = await nas.Changes(epoch, cursor, backoff > 1 ? 0 : 50, ct);
                if (page == null)
                {
                    epoch = null;
                    continue;
                }
                await Local.Gate.WaitAsync(ct);
                try
                {
                    foreach (var c in page.Changes)
                        Apply(c);
                    Interlocked.Exchange(ref applied, page.Cursor);
                }
                finally
                {
                    Local.Gate.Release();
                }
                cursor = page.Cursor;
                foreach (var f in SyncRoot.Folders)
                    profile.SetSynced(f, epoch, cursor);
                backoff = 1;
                Status(Phase.UpToDate);
            }
            catch (Exception e) when (e is not AuthException && !ct.IsCancellationRequested)
            {
                Log.Error($"{e.Message}; retrying in {backoff} s");
                Status(Phase.Offline, e.Message);
                await Task.Delay(TimeSpan.FromSeconds(backoff), ct);
                backoff = Math.Min(backoff * 2, 60);
            }
        }
    }

    // Everything inside, hidden files too; folders it may not list (Documents' old "My Music" links) are skipped.
    public static readonly EnumerationOptions Deep = new() { RecurseSubdirectories = true, AttributesToSkip = 0, IgnoreInaccessible = true };

    static async Task<(int Created, int Updated, int Removed)> Reconcile(Nas nas, Profile profile, Folder f, List<Entry> all, string epoch, CancellationToken ct)
    {
        var synced = profile.Synced(f);
        if (f.Backup && !all.Exists(e => e.Path == f.Nas))
        {
            if (synced is { } s && s.Epoch == epoch)
            {
                // There at the last sync, so removed on the NAS (in the web's Computers, say) while this PC was off.
                Engine.Dropped(f);
                return default;
            }
            // New here, or the NAS lost it: what is on this PC is the copy to keep, so all of it goes up (again).
            // Nothing here is ever deleted for being missing on the NAS.
            await Local.EnsureFolder(nas, f.Nas, ct);
            foreach (var file in Local.Walk(f.Local, filesOnly: true))
            {
                try
                {
                    using var h = SyncRoot.Open(file, ShareAll);
                    if (SyncRoot.Info(h) is { InSync: true })
                        SyncRoot.ClearInSync(h);
                }
                catch (Exception x) when (x is IOException or UnauthorizedAccessException or System.Runtime.InteropServices.COMException) { }
            }
            return default;
        }
        var root = f.Local;
        var entries = all.Where(e => e.Path != f.Nas && SyncRoot.Owner(e.Path) == f).ToList();
        var known = entries.Select(e => SyncRoot.LocalPath(e.Path)).ToHashSet(StringComparer.OrdinalIgnoreCase);
        var strays = Local.Walk(root).Where(p => !known.Contains(p) && !SyncRoot.Outside(p) && !Local.Foreign(p)).ToList();
        var done = new List<string>(); // NAS paths now settled by an offline delete or rename (with everything inside)
        var moved = new List<string>(); // local paths the NAS now has, through such a rename
        bool Under(string path, List<string> tops) => tops.Exists(t => path.Equals(t, StringComparison.OrdinalIgnoreCase)
            || path.StartsWith(t + (t.Contains('\\') ? '\\' : '/'), StringComparison.OrdinalIgnoreCase));

        // An empty folder is a fresh start (just created, or deleted with the app off), never a pile of deletes.
        if (synced is { } last && last.Epoch == epoch && strays.Count(p => !Local.Scratch(Path.GetFileName(p))) + entries.Count(e => Path.Exists(SyncRoot.LocalPath(e.Path))) > 0)
        {
            // Here at the last sync, gone now: deleted or renamed on this PC while the app wasn't running.
            var gone = entries.Where(e => e.Rev <= last.Cursor && !Path.Exists(SyncRoot.LocalPath(e.Path))).ToList();
            int affected = gone.Count; // everything inside counts: five wiped top folders are not five items
            var gonePaths = gone.Select(e => e.Path).ToList();
            gone = gone.Where(e => !Under(e.Path, gonePaths.Where(p => p != e.Path).ToList())).ToList(); // the topmost ones
            if ((affected > 20 && affected * 10 > entries.Count) || (affected >= 5 && affected * 2 > entries.Count))
            {
                // A wiped or moved folder, not a tidy-up: restore rather than delete on the NAS.
                Engine.Notify($"{affected:N0} items were missing from {(f.Backup ? $"“{f.Name}”" : "the Aetheris folder")}, so they were restored from your NAS. If you deleted them on purpose while Aetheris wasn't running, delete them again.");
                gone.Clear();
            }
            // A renamed item still carries its identity (the NAS rev) at its new place.
            var byRev = new Dictionary<long, string>();
            foreach (var p in strays)
            {
                try
                {
                    using var h = SyncRoot.Open(p, ShareAll);
                    if (SyncRoot.Info(h) is { } info)
                        byRev.TryAdd(info.Rev, p);
                }
                catch (Exception x) when (x is IOException or UnauthorizedAccessException) { }
            }
            foreach (var e in gone)
            {
                try
                {
                    if (byRev.TryGetValue(e.Rev, out var to))
                    {
                        await nas.Move(e.Path, SyncRoot.Rel(to), e.Dir ? 0 : e.Rev, ct);
                        moved.Add(to);
                        Log.Info($"→ {e.Path} → {SyncRoot.Rel(to)} (renamed while Aetheris wasn't running)");
                    }
                    else
                    {
                        await nas.Delete(e.Path, e.Dir ? 0 : e.Rev, ct);
                        Log.Info($"✕ {e.Path} (deleted while Aetheris wasn't running; in the NAS trash)");
                    }
                    done.Add(e.Path);
                }
                catch (ConflictException)
                {
                    Log.Info($"{e.Path}: changed on the NAS since, so it was brought back"); // an edit beats a delete
                }
                catch (HttpRequestException x) when (x.StatusCode == System.Net.HttpStatusCode.NotFound)
                {
                    done.Add(e.Path);
                }
            }
        }

        int deleted = done.Count - moved.Count;
        if (deleted > 0)
            Engine.Notify(deleted == 1 ? $"“{Path.GetFileName(done.First(d => !moved.Exists(m => SyncRoot.Rel(m) == d)))}” was deleted while Aetheris wasn't running, so it went to the NAS trash."
                : $"{deleted} items deleted while Aetheris wasn't running went to the NAS trash.");
        var existing = SyncRoot.CreatePlaceholders(entries.Where(e => !Under(e.Path, done)), out int created);
        long before = synced is { } had && had.Epoch == epoch ? had.Cursor : 0; // NAS versions this PC had before it stopped
        int updated = existing.Count(e => TryUpsert(e, before));
        int removed = strays
            .Where(p => !Under(p, moved))
            .OrderByDescending(p => p.Length) // contents before their folder
            .Count(p => Remove(p));
        return (created, updated, removed);
    }

    static void Apply(Change c)
    {
        try
        {
            // A folder from elsewhere on this PC deleted or moved away on the NAS as a whole (or Computers above it):
            // this PC stops syncing it, and its files stay here.
            foreach (var f in SyncRoot.Folders.Where(f => f.Backup))
                if ((c.Op == "delete" && SyncRoot.Under(f.Nas, c.Path)) || (c.Op == "move" && c.From != null && SyncRoot.Under(f.Nas, c.From)))
                {
                    Engine.Dropped(f);
                    return;
                }
            var local = SyncRoot.LocalPath(c.Path);
            if (c.Op == "move" && c.From != null && SyncRoot.LocalPath(c.From) is { } from)
            {
                if (local != null && SyncRoot.Of(from) == SyncRoot.Of(local))
                    Move(from, local);
                else
                    Remove(from); // into another folder or off this PC: here it is a delete (and a new item there)
            }
            if (local == null || SyncRoot.Folders.Any(f => f.Local.Equals(local, StringComparison.OrdinalIgnoreCase)))
                return; // not on this PC, or a folder itself
            if (c.Entry != null)
                Upsert(c.Entry);
            else if (c.Op == "delete")
                Remove(local);
        }
        catch (Exception e)
        {
            // shortcut: a skipped change waits for the next start's full reconcile; retry sooner if that proves too slow.
            Log.Error($"applying NAS {c.Op} of {c.Path}: {e.GetType().Name}: {e.Message}");
        }
    }

    static bool TryUpsert(Entry e, long known)
    {
        try { return Upsert(e, known); }
        catch (Exception x) { Log.Error($"{e.Path}: {x.Message}"); return false; }
    }

    // Brings one placeholder to the NAS state. Content that changed is dropped (dehydrated) so the next
    // open downloads the new bytes; a move or a touch only rewrites the identity.
    // known: the cursor this PC had synced to before (at start); a plain file in the place of an older NAS version replaced it.
    static bool Upsert(Entry e, long known = 0)
    {
        skipped.TryRemove(e.Path, out _); // marked again below if this version can't reach this PC
        var local = SyncRoot.LocalPath(e.Path)!;
        if (!Path.Exists(local))
        {
            SyncRoot.CreatePlaceholders([e], out _);
            Local.Touch(local);
            Log.Info($"+ {e.Path}");
            if (!e.Dir)
                Engine.Record("new", e.Path, size: e.Size);
            return true;
        }
        Placeholder? cur;
        using (var h = SyncRoot.Open(local, ShareAll))
        {
            cur = SyncRoot.Info(h);
            if (cur == null && (e.Dir ? Directory.Exists(local) : Same(local, e) && SyncRoot.Links(h) <= 1))
            {
                // Already here as a plain item (a folder added again, say): it becomes the NAS item's placeholder,
                // instead of a conflict copy of itself.
                SyncRoot.Convert(h, e, inSync: true);
                return true;
            }
            if (cur is not { } p || p.Rev == e.Rev)
            {
                if (cur == null && e.Rev > known)
                    skipped[e.Path] = 0;
                return false; // shortcut: a same-named new local item is left for the upload direction's conflict copy
            }
            if (p.Hash == (e.Hash ?? ""))
            {
                // Same content (moved or touched on the NAS). A local edit keeps its bytes: only the identity moves on.
                if (p.InSync)
                    SyncRoot.Update(h, e, CF_UPDATE_FLAGS.CF_UPDATE_FLAG_MARK_IN_SYNC);
                else
                    SyncRoot.SetIdentity(h, e, inSync: false);
                return true;
            }
            if (!p.InSync)
            {
                // Changed on both sides: the upload turns the local edit into a conflict copy.
                Log.Info($"{e.Path}: changed here and on the NAS; kept the local copy");
                skipped[e.Path] = 0;
                return false;
            }
        }
        // Dehydrating needs an exclusive handle: it fails while an app has the file open, and VERIFY_IN_SYNC
        // refuses if a local edit landed since the check above.
        using (var h = SyncRoot.Open(local, 0))
        {
            // Windows refuses to dehydrate a pinned file: unpin for the moment. Pinning it again makes the
            // uploader's watcher see it and download the new bytes.
            bool pinned = (File.GetAttributes(local) & SyncRoot.Pinned) != 0;
            if (pinned)
                SyncRoot.SetPin(h, CF_PIN_STATE.CF_PIN_STATE_UNSPECIFIED);
            try
            {
                SyncRoot.Update(h, e, CF_UPDATE_FLAGS.CF_UPDATE_FLAG_MARK_IN_SYNC | CF_UPDATE_FLAGS.CF_UPDATE_FLAG_VERIFY_IN_SYNC | CF_UPDATE_FLAGS.CF_UPDATE_FLAG_DEHYDRATE);
            }
            finally
            {
                if (pinned)
                    SyncRoot.SetPin(h, CF_PIN_STATE.CF_PIN_STATE_PINNED);
            }
        }
        Log.Info($"↻ {e.Path}");
        Engine.Record("changed", e.Path, size: e.Size);
        Local.Touch(local); // a folder from elsewhere on the PC downloads the new bytes right away
        return true;
    }

    // Size and modified time to the millisecond, as the upload recorded them: the file the NAS has.
    // shortcut: no hash here (the NAS hashes with BLAKE3); identical size and time with other content is taken as the same.
    public static bool Same(string local, Entry e) =>
        new FileInfo(local) is { Exists: true } f && f.Length == e.Size && new DateTimeOffset(f.LastWriteTimeUtc).ToUnixTimeMilliseconds() == e.Mtime;

    // Deletes in-sync placeholders (a folder once it is empty). Our handle shares only delete,
    // so nobody can write to the file between the in-sync check and the delete.
    static bool Remove(string local)
    {
        try
        {
            return RemoveNow(local);
        }
        catch (Exception e) when (e is UnauthorizedAccessException or IOException)
        {
            // Usually already being deleted here (our own delete, waiting for the NAS to confirm); anything
            // else that can't go now is cleaned up by the next start's reconcile.
            return false;
        }
    }

    static bool RemoveNow(string local)
    {
        if (Local.Foreign(local))
            return false; // a link to elsewhere: never followed, never ours
        if (Directory.Exists(local))
        {
            foreach (var child in Directory.EnumerateFileSystemEntries(local).ToList())
                Remove(child);
            bool ours;
            using (var h = SyncRoot.Open(local, ShareAll))
                ours = SyncRoot.Info(h) != null;
            if (!ours || Directory.EnumerateFileSystemEntries(local).Any())
                return false;
            Directory.Delete(local);
        }
        else if (File.Exists(local)) // already gone here, e.g. our own delete coming back from the NAS
        {
            using var h = SyncRoot.Open(local, FILE_SHARE_MODE.FILE_SHARE_DELETE);
            var p = SyncRoot.Info(h);
            if (p is not { InSync: true })
            {
                if (p != null)
                    Log.Info($"{local}: deleted on the NAS, kept here (changed on this PC)");
                return false; // a new local file: not ours to delete
            }
            File.Delete(local);
        }
        else
            return false;
        Log.Info($"− {local}");
        Engine.Record("removed", SyncRoot.Rel(local));
        return true;
    }

    static void Move(string from, string to)
    {
        if (!Path.Exists(from) || (Path.Exists(to) && !string.Equals(from, to, StringComparison.OrdinalIgnoreCase)))
            return;
        if (Directory.Exists(from))
            Directory.Move(from, to);
        else
            File.Move(from, to);
        Log.Info($"→ {to}");
        Engine.Record("moved", SyncRoot.Rel(from), SyncRoot.Rel(to));
    }
}
