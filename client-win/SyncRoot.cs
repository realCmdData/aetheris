using System.Buffers;
using System.Collections.Concurrent;
using System.Net;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Security.Principal;
using System.Text;
using Microsoft.Win32.SafeHandles;
using Windows.Security.Cryptography;
using Windows.Storage;
using Windows.Storage.Provider;
using Windows.Win32;
using Windows.Win32.Foundation;
using Windows.Win32.Storage.CloudFilters;
using Windows.Win32.Storage.FileSystem;

namespace Aetheris;

// What a placeholder remembers about the NAS copy it stands for: FileIdentity is "rev\nhash".
// The NAS path is wherever the placeholder sits, so moving a folder never touches its contents.
record struct Placeholder(long Rev, string Hash, bool InSync);

// A folder this PC keeps in sync: the Aetheris folder (NAS path "") or one from elsewhere on the PC, which the
// NAS keeps under Computers/<this PC>/<name> (the web's Computers section). Each is a sync root of its own.
sealed record Folder(string Local, string Nas)
{
    public const string Computers = "Computers";
    public bool Backup => Nas != "";
    public string Name => Backup ? Nas[(Nas.LastIndexOf('/') + 1)..] : "Aetheris";
    // Names the folder's registration and state file; stable for its path.
    public string Id => Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(Encoding.UTF8.GetBytes(Local.ToLowerInvariant())))[..12].ToLowerInvariant();
}

// The cloud folders in Explorer: registration, placeholders, and downloading a file's bytes when it is opened.
static class SyncRoot
{
    const int Chunk = 1 << 20; // a 4 KB multiple: every transfer but the last must be 4 KB aligned
    // The NAS encrypts each connection on one core (no AES-NI, and Windows' TLS offers no ChaCha20): about
    // 17 MB/s each. Big files come in this many ranged parts at once, leaving the J1900 one core of four.
    const int Parts = 3;
    const long PartMin = 16 << 20;
    static Nas? nas;
    static readonly ConcurrentDictionary<string, CF_CONNECTION_KEY> keys = new(StringComparer.OrdinalIgnoreCase);
    static Folder[] folders = []; // the connected ones; replaced whole, so callbacks always see a complete list

    public static Nas Client => nas!;
    public static int Downloads => fetches.Count;
    public static Folder[] Folders => folders;

    // A new device token (after pairing again) takes effect for the next download.
    public static void Use(Nas client) => nas = client;

    public static void Track(Folder f) => folders = [.. folders.Where(x => x != f), f];
    public static void Untrack(Folder f) => folders = [.. folders.Where(x => x != f)];

    // The folder holding a NAS path: a folder from elsewhere on this PC for its part of Computers, the Aetheris folder
    // for everything outside Computers, and null for the rest (other PCs, folders not synced here).
    public static Folder? Owner(string nas)
    {
        foreach (var f in folders)
            if (f.Backup && Under(nas, f.Nas))
                return f;
        return Under(nas, Folder.Computers) ? null : folders.FirstOrDefault(f => !f.Backup);
    }

    // path is top itself or inside it (NAS paths).
    public static bool Under(string path, string top) => path == top || path.StartsWith(top + "/", StringComparison.Ordinal);

    // The folder a local path is in.
    public static Folder? Of(string local) =>
        folders.Where(f => local.Equals(f.Local, StringComparison.OrdinalIgnoreCase) || local.StartsWith(f.Local + "\\", StringComparison.OrdinalIgnoreCase))
            .MaxBy(f => f.Local.Length);

    // In a synced folder but not in its part of the NAS: a "Computers" folder at the top of the Aetheris folder
    // (on the NAS that name is the Computers section). It stays on this PC.
    public static bool Outside(string local) => Of(local) is not { } f || Owner(Rel(local)) != f;

    sealed class Download(string path, long size)
    {
        public readonly CancellationTokenSource Cancel = new();
        public readonly string Path = path;
        public readonly long Size = size;
        public long Done;
    }

    static readonly ConcurrentDictionary<long, Download> fetches = new();

    public static IEnumerable<Engine.Transfer> Transfers() =>
        fetches.Values.Select(d => new Engine.Transfer("down", d.Path, Interlocked.Read(ref d.Done), d.Size)).ToList();

    static string Id(Profile p, Folder f) => $"Aetheris!{WindowsIdentity.GetCurrent().User!.Value}!{p.Name}{(f.Backup ? "-" + f.Id : "")}";

    // Throws when Windows refuses (a folder another sync app already has, say).
    public static async Task Register(Profile p, Folder f)
    {
        if (!f.Backup)
        {
            Directory.CreateDirectory(f.Local);
            try { FolderIcon(f.Local); }
            catch (Exception e) when (e is IOException or UnauthorizedAccessException) { Log.Error($"folder icon: {e.Message}"); }
        }
        // With the package identity, registering again ties the folder to the package (its pane entry). Never by
        // unregistering first: that deletes every online-only placeholder and turns downloaded files into plain ones.
        var mode = Packaged() ? "packaged" : "unpackaged";
        var before = f.Backup ? mode : p.RegisteredAs ?? "unpackaged";
        if (before != mode)
            Log.Info($"registering the folder, now {mode}");
        var info = new StorageProviderSyncRootInfo
        {
            Id = Id(p, f),
            Path = await StorageFolder.GetFolderFromPathAsync(f.Local),
            DisplayNameResource = f.Backup ? $"{p.DisplayName} · {f.Name}" : p.DisplayName,
            // A folder from elsewhere on the PC keeps its own icon in Explorer (Downloads' arrow); the Aetheris folder wears the logo.
            IconResource = f.Backup ? Aetheris.FolderIcon.Own(f.Local) ?? Environment.ExpandEnvironmentVariables(Aetheris.FolderIcon.Plain) : Environment.ProcessPath + ",0",
            HydrationPolicy = StorageProviderHydrationPolicy.Full,
            HydrationPolicyModifier = StorageProviderHydrationPolicyModifier.None,
            PopulationPolicy = StorageProviderPopulationPolicy.AlwaysFull,
            InSyncPolicy = StorageProviderInSyncPolicy.FileCreationTime | StorageProviderInSyncPolicy.DirectoryCreationTime,
            HardlinkPolicy = StorageProviderHardlinkPolicy.None,
            AllowPinning = !f.Backup, // your own folders stay here in full: no "Free up space" on them
            Version = "1",
            Context = CryptographicBuffer.ConvertStringToBinary(p.Server?.ToString() ?? "", BinaryStringEncoding.Utf8),
        };
        try
        {
            StorageProviderSyncRootManager.Register(info);
            if (!f.Backup)
                p.RegisteredAs = mode;
        }
        catch (Exception e) when (before != mode)
        {
            // Windows kept the earlier registration: the folder still works through it, just without the pane entry.
            Log.Error($"registering the folder {mode} was refused ({e.Message}); keeping the {before} registration");
        }
    }

    public static void Unregister(Profile p, Folder f) => StorageProviderSyncRootManager.Unregister(Id(p, f));

    // True when Windows gave this process the sparse package's identity (client-win/package).
    public static bool Packaged()
    {
        uint n = 0;
        return (uint)PInvoke.GetCurrentPackageFullName(ref n, default) != 15700; // APPMODEL_ERROR_NO_PACKAGE
    }

    // The folder wears the Aetheris logo wherever Explorer shows it: a Quick access pin, Home, the profile folder.
    // The usual desktop.ini plus the folder's read-only bit (Explorer's cue that it is customised); never uploaded.
    static unsafe void FolderIcon(string root)
    {
        var ini = Path.Combine(root, "desktop.ini");
        var text = $"[.ShellClassInfo]\r\nIconResource={Environment.ProcessPath},0\r\n";
        if (File.Exists(ini) && File.ReadAllText(ini) == text)
            return;
        if (File.Exists(ini))
            File.SetAttributes(ini, System.IO.FileAttributes.Normal);
        File.WriteAllText(ini, text, Encoding.Unicode);
        File.SetAttributes(ini, System.IO.FileAttributes.Hidden | System.IO.FileAttributes.System);
        var dir = new DirectoryInfo(root);
        dir.Attributes |= System.IO.FileAttributes.ReadOnly;
        fixed (char* path = root)
            PInvoke.SHChangeNotify(Windows.Win32.UI.Shell.SHCNE_ID.SHCNE_UPDATEITEM, Windows.Win32.UI.Shell.SHCNF_FLAGS.SHCNF_PATHW, path, null);
    }

    static unsafe CF_CALLBACK_REGISTRATION* table;

    // Track the folder first: callbacks can arrive as soon as it is connected.
    public static unsafe void Connect(Folder f)
    {
        if (table == null)
        {
            // The table must outlive every connection, so it lives in native memory for the whole process.
            table = (CF_CALLBACK_REGISTRATION*)NativeMemory.AllocZeroed(5, (nuint)sizeof(CF_CALLBACK_REGISTRATION));
            table[0] = new() { Type = CF_CALLBACK_TYPE.CF_CALLBACK_TYPE_FETCH_DATA, Callback = &OnFetchData };
            table[1] = new() { Type = CF_CALLBACK_TYPE.CF_CALLBACK_TYPE_CANCEL_FETCH_DATA, Callback = &OnCancelFetchData };
            table[2] = new() { Type = CF_CALLBACK_TYPE.CF_CALLBACK_TYPE_NOTIFY_DELETE, Callback = &OnDelete };
            table[3] = new() { Type = CF_CALLBACK_TYPE.CF_CALLBACK_TYPE_NOTIFY_RENAME, Callback = &OnRename };
            table[4] = new() { Type = CF_CALLBACK_TYPE.CF_CALLBACK_TYPE_NONE };
        }
        CF_CONNECTION_KEY key;
        fixed (char* r = f.Local)
            PInvoke.CfConnectSyncRoot(r, table, null,
                CF_CONNECT_FLAGS.CF_CONNECT_FLAG_REQUIRE_PROCESS_INFO | CF_CONNECT_FLAGS.CF_CONNECT_FLAG_REQUIRE_FULL_FILE_PATH
                    | CF_CONNECT_FLAGS.CF_CONNECT_FLAG_BLOCK_SELF_IMPLICIT_HYDRATION,
                &key).ThrowOnFailure();
        keys[f.Local] = key;
    }

    public static void Disconnect(Folder f)
    {
        if (keys.TryRemove(f.Local, out var key))
            PInvoke.CfDisconnectSyncRoot(key);
    }

    public static void Disconnect()
    {
        foreach (var local in keys.Keys)
            if (keys.TryRemove(local, out var key))
                PInvoke.CfDisconnectSyncRoot(key);
    }

    // Where a NAS path is on this PC; null when it isn't here (see Owner).
    public static string? LocalPath(string nas)
    {
        if (Owner(nas) is not { } f)
            return null;
        var rest = f.Backup ? nas[f.Nas.Length..].TrimStart('/') : nas;
        return rest == "" ? f.Local : Path.Combine(f.Local, rest.Replace('/', '\\'));
    }

    static byte[] Identity(Entry e) => Encoding.UTF8.GetBytes($"{e.Rev}\n{e.Hash}");

    // attributes 0 keeps the item's current ones (pinning lives there).
    static CF_FS_METADATA Metadata(Entry e, uint attributes)
    {
        long t = DateTimeOffset.FromUnixTimeMilliseconds(e.Mtime).UtcDateTime.ToFileTimeUtc();
        return new()
        {
            FileSize = e.Dir ? 0 : e.Size,
            BasicInfo = new() { CreationTime = t, LastWriteTime = t, ChangeTime = t, LastAccessTime = t, FileAttributes = attributes },
        };
    }

    // One CfCreatePlaceholders call per folder. The tree lists parents before children, and GroupBy keeps
    // first-seen order, so a folder is always created before its contents. Returns the items that already existed.
    public static unsafe List<Entry> CreatePlaceholders(IEnumerable<Entry> entries, out int created)
    {
        var existing = new List<Entry>();
        created = 0;
        foreach (var folder in entries.GroupBy(e => e.Path.LastIndexOf('/') is var i and >= 0 ? e.Path[..i] : ""))
        {
            if (LocalPath(folder.Key) is not { } dir)
                continue; // not here (callers pass only what is)
            var items = folder.ToArray();
            var infos = new CF_PLACEHOLDER_CREATE_INFO[items.Length];
            var pins = new List<GCHandle>(items.Length * 2);
            try
            {
                for (int i = 0; i < items.Length; i++)
                {
                    var e = items[i];
                    var identity = Identity(e);
                    var name = GCHandle.Alloc(e.Path[(e.Path.LastIndexOf('/') + 1)..], GCHandleType.Pinned);
                    var id = GCHandle.Alloc(identity, GCHandleType.Pinned);
                    pins.Add(name);
                    pins.Add(id);
                    infos[i] = new()
                    {
                        RelativeFileName = (char*)name.AddrOfPinnedObject(),
                        FsMetadata = Metadata(e, (uint)(e.Dir ? System.IO.FileAttributes.Directory : System.IO.FileAttributes.Normal)),
                        FileIdentity = (void*)id.AddrOfPinnedObject(),
                        FileIdentityLength = (uint)identity.Length,
                        Flags = CF_PLACEHOLDER_CREATE_FLAGS.CF_PLACEHOLDER_CREATE_FLAG_MARK_IN_SYNC
                            | (e.Dir ? CF_PLACEHOLDER_CREATE_FLAGS.CF_PLACEHOLDER_CREATE_FLAG_DISABLE_ON_DEMAND_POPULATION : 0),
                    };
                }
                fixed (CF_PLACEHOLDER_CREATE_INFO* p = infos)
                fixed (char* d = Long(dir))
                    PInvoke.CfCreatePlaceholders(d, p, (uint)infos.Length, CF_CREATE_FLAGS.CF_CREATE_FLAG_NONE, null);
            }
            finally
            {
                foreach (var h in pins)
                    h.Free();
            }
            for (int i = 0; i < infos.Length; i++)
            {
                if (infos[i].Result.Succeeded)
                    created++;
                else if (infos[i].Result == AlreadyExists)
                    existing.Add(items[i]);
                else
                    Log.Error($"{items[i].Path}: placeholder failed ({infos[i].Result})");
            }
        }
        return existing;
    }

    static readonly HRESULT AlreadyExists = (HRESULT)unchecked((int)0x800700B7);

    // A handle that reads and updates placeholder state without touching file data (folders too).
    // share: FILE_SHARE_DELETE alone keeps everyone else from writing while we decide.
    // Throws when it can't open: an invalid handle would read as "not a placeholder" and look like a new file.
    public static SafeFileHandle Open(string path, FILE_SHARE_MODE share)
    {
        var h = PInvoke.CreateFile(Long(path),0x80 | 0x40000 /* FILE_READ_ATTRIBUTES | WRITE_DAC */, share, null,
            FILE_CREATION_DISPOSITION.OPEN_EXISTING, FILE_FLAGS_AND_ATTRIBUTES.FILE_FLAG_BACKUP_SEMANTICS, null);
        if (h.IsInvalid)
            throw Marshal.GetExceptionForHR(Marshal.GetHRForLastWin32Error())!; // access denied, sharing violation, gone, ...
        return h;
    }

    // Paths past Windows' old 260-character limit (deep node_modules, say) need the \\?\ form in direct Win32 calls;
    // .NET's own file APIs add it themselves.
    static string Long(string path) => path.Length >= 240 && !path.StartsWith(@"\\") ? @"\\?\" + path : path;

    // How many names a file has. Windows' cloud files can't be hard-linked: such a file never becomes a placeholder
    // (ERROR_CLOUD_FILE_INCOMPATIBLE_HARDLINKS), e.g. Rust's and pnpm's build outputs.
    public static uint Links(SafeHandle h) =>
        PInvoke.GetFileInformationByHandle(h, out var info) ? info.nNumberOfLinks : 1;

    // "Always keep on this device" and "Free up space" in Explorer only set these; the bytes are ours to move.
    public const System.IO.FileAttributes Pinned = (System.IO.FileAttributes)0x80000, Unpinned = (System.IO.FileAttributes)0x100000;
    public const System.IO.FileAttributes Dehydrated = (System.IO.FileAttributes)0x400000; // FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS

    // Downloads the whole file through our own FETCH_DATA callback; returns once it is here.
    public static void Hydrate(SafeHandle h) =>
        PInvoke.CfHydratePlaceholder(h, 0, -1 /* to the end */, CF_HYDRATE_FLAGS.CF_HYDRATE_FLAG_NONE).ThrowOnFailure();

    // Needs an exclusive handle, and an in-sync file: unsent edits are never thrown away.
    public static void Dehydrate(SafeHandle h) =>
        PInvoke.CfDehydratePlaceholder(h, 0, -1, CF_DEHYDRATE_FLAGS.CF_DEHYDRATE_FLAG_NONE).ThrowOnFailure();

    public static void SetPin(SafeHandle h, CF_PIN_STATE state) =>
        PInvoke.CfSetPinState(h, state, CF_SET_PIN_FLAGS.CF_SET_PIN_FLAG_NONE).ThrowOnFailure();

    // null: not one of our placeholders (a new local file, say).
    public static unsafe Placeholder? Info(SafeHandle h)
    {
        Span<byte> buf = stackalloc byte[1024];
        if (PInvoke.CfGetPlaceholderInfo(h, CF_PLACEHOLDER_INFO_CLASS.CF_PLACEHOLDER_INFO_BASIC, buf, out _).Failed)
            return null;
        fixed (byte* b = buf)
        {
            var info = (CF_PLACEHOLDER_BASIC_INFO*)b;
            var id = Encoding.UTF8.GetString(info->FileIdentity.AsSpan((int)info->FileIdentityLength)).Split('\n');
            return id.Length == 2 && long.TryParse(id[0], out var rev)
                ? new(rev, id[1], info->InSyncState == CF_IN_SYNC_STATE.CF_IN_SYNC_STATE_IN_SYNC)
                : null;
        }
    }

    // Only for in-sync placeholders: the metadata sets the file size, which would cut a local edit short.
    public static void Update(SafeHandle h, Entry e, CF_UPDATE_FLAGS flags) =>
        PInvoke.CfUpdatePlaceholder(h, Metadata(e, 0), Identity(e), default, flags).ThrowOnFailure();

    // Records which NAS revision the item is now based on, without touching its data or times.
    public static void SetIdentity(SafeHandle h, Entry e, bool inSync) =>
        PInvoke.CfUpdatePlaceholder(h, null, Identity(e), default,
            inSync ? CF_UPDATE_FLAGS.CF_UPDATE_FLAG_MARK_IN_SYNC : CF_UPDATE_FLAGS.CF_UPDATE_FLAG_NONE).ThrowOnFailure();

    public static void MarkInSync(SafeHandle h) =>
        PInvoke.CfUpdatePlaceholder(h, null, default, default, CF_UPDATE_FLAGS.CF_UPDATE_FLAG_MARK_IN_SYNC).ThrowOnFailure();

    // The uploader then sends it again (the NAS lost it, say).
    public static void ClearInSync(SafeHandle h) =>
        PInvoke.CfUpdatePlaceholder(h, null, default, default, CF_UPDATE_FLAGS.CF_UPDATE_FLAG_CLEAR_IN_SYNC).ThrowOnFailure();

    // A new local file or folder becomes a placeholder once the NAS has it.
    public static void Convert(SafeHandle h, Entry e, bool inSync) =>
        PInvoke.CfConvertToPlaceholder(h, Identity(e),
            inSync ? CF_CONVERT_FLAGS.CF_CONVERT_FLAG_MARK_IN_SYNC : CF_CONVERT_FLAGS.CF_CONVERT_FLAG_NONE).ThrowOnFailure();

    // The NAS path of a local item in a synced folder (a folder's own path gives its NAS path).
    public static string Rel(string full)
    {
        var f = Of(full) ?? throw new ArgumentException($"{full} is not in a synced folder");
        var rel = Path.GetRelativePath(f.Local, full).Replace('\\', '/');
        return rel == "." ? f.Nas : f.Nas == "" ? rel : $"{f.Nas}/{rel}";
    }

    // A delete or rename in Explorer, asked before it happens: it goes to the NAS first, and only a NAS
    // that refuses (offline, say) stops it. Our own deletes and renames (applying NAS changes) just pass, and so
    // do items that never went to the NAS (Outside).
    record Notice(CF_CONNECTION_KEY Connection, long TransferKey, long RequestKey, long Rev, bool Dir, string Path, string? Target, bool Ours);

    static unsafe Notice Read(CF_CALLBACK_INFO* info, bool dir, string? target)
    {
        var full = info->VolumeDosName.ToString() + info->NormalizedPath.ToString();
        // Outside also covers paths in no synced folder: a file being deleted can report C:\$Extend\$Deleted\….
        // Such a notice must still be answered (Forward does), or whoever deletes waits for a minute.
        bool outside = Outside(full);
        return new(info->ConnectionKey, info->TransferKey, info->RequestKey,
            long.TryParse(Encoding.UTF8.GetString((byte*)info->FileIdentity, (int)info->FileIdentityLength).Split('\n')[0], out var rev) ? rev : 0,
            dir, outside ? full : Rel(full), target,
            outside || (info->ProcessInfo != null && info->ProcessInfo->ProcessId == Environment.ProcessId));
    }

    [UnmanagedCallersOnly(CallConvs = [typeof(CallConvStdcall)])]
    static unsafe void OnDelete(CF_CALLBACK_INFO* info, CF_CALLBACK_PARAMETERS* p)
    {
        try
        {
            var n = Read(info, (p->Delete.Flags & CF_CALLBACK_DELETE_FLAGS.CF_CALLBACK_DELETE_FLAG_IS_DIRECTORY) != 0, null);
            _ = Task.Run(() => Forward(n, CF_OPERATION_TYPE.CF_OPERATION_TYPE_ACK_DELETE));
        }
        catch (Exception e)
        {
            Log.Error($"delete callback: {e.Message}");
        }
    }

    [UnmanagedCallersOnly(CallConvs = [typeof(CallConvStdcall)])]
    static unsafe void OnRename(CF_CALLBACK_INFO* info, CF_CALLBACK_PARAMETERS* p)
    {
        try
        {
            var flags = p->Rename.Flags;
            // Out of the folder (the Recycle Bin, say) is a delete as far as the NAS is concerned.
            var to = info->VolumeDosName.ToString() + p->Rename.TargetPath.ToString();
            var target = (flags & CF_CALLBACK_RENAME_FLAGS.CF_CALLBACK_RENAME_FLAG_TARGET_IN_SCOPE) != 0 && !Outside(to) ? Rel(to) : null;
            var n = Read(info, (flags & CF_CALLBACK_RENAME_FLAGS.CF_CALLBACK_RENAME_FLAG_IS_DIRECTORY) != 0, target);
            _ = Task.Run(() => Forward(n, CF_OPERATION_TYPE.CF_OPERATION_TYPE_ACK_RENAME));
        }
        catch (Exception e)
        {
            Log.Error($"rename callback: {e.Message}");
        }
    }

    // Windows drops a file's in-sync mark when it is renamed. Remember files that were in sync, so the
    // uploader can restore the mark instead of sending unchanged bytes up again.
    static readonly ConcurrentDictionary<string, (long Size, DateTime Written)> renamed = new(StringComparer.OrdinalIgnoreCase);

    public static bool WasInSync(string local)
    {
        var f = new FileInfo(local);
        return renamed.TryRemove(local, out var m) && f.Exists && m.Size == f.Length && m.Written == f.LastWriteTimeUtc;
    }

    // An app's safe save (Word: rename to ~WRL….tmp, write the new file under the old name, delete the temp).
    // The NAS keeps the original throughout, and the new file goes up as an edit of it, so its versions survive.
    static readonly ConcurrentDictionary<string, (string Original, long Rev)> swapped = new(StringComparer.OrdinalIgnoreCase); // temp → original
    static readonly ConcurrentDictionary<string, long> replacing = new(StringComparer.OrdinalIgnoreCase); // original → its NAS rev

    public static long? Replacing(string local) => replacing.TryGetValue(local, out var rev) ? rev : null;
    public static void Replaced(string local) => replacing.TryRemove(local, out _);

    static async Task Forward(Notice n, CF_OPERATION_TYPE ack)
    {
        NTSTATUS status = default;
        bool skip = n.Ours; // our own deletes and renames are already the NAS's
        string path = n.Path; // what to move or delete on the NAS
        if (!n.Ours && !n.Dir)
        {
            var src = LocalPath(n.Path)!;
            if (n.Target != null && Local.Scratch(Path.GetFileName(n.Target)))
            {
                swapped[LocalPath(n.Target)!] = (src, n.Rev);
                replacing[src] = n.Rev;
                skip = true;
            }
            else if (n.Target == null && swapped.TryRemove(src, out var save))
            {
                // The temp going away: only if nothing took the original's place was the original deleted.
                if (Path.Exists(save.Original))
                    skip = true;
                else
                {
                    path = Rel(save.Original);
                    replacing.TryRemove(save.Original, out _);
                }
            }
        }
        if (!skip && n.Target != null && !n.Dir)
        {
            var src = LocalPath(n.Path)!;
            try
            {
                using var h = Open(src, Remote.ShareAll);
                if (Info(h) is { InSync: true })
                {
                    var f = new FileInfo(src);
                    renamed[LocalPath(n.Target)!] = (f.Length, f.LastWriteTimeUtc);
                }
            }
            catch (Exception e) when (e is IOException or UnauthorizedAccessException)
            {
                // Can't look: the uploader compares with the NAS instead.
            }
        }
        // Folders go without a revision check: a whole folder lands in the NAS trash, so nothing is lost.
        long baseRev = n.Dir ? 0 : n.Rev;
        if (!skip)
        {
            try
            {
                if (n.Target != null)
                {
                    await nas!.Move(path, n.Target, baseRev, CancellationToken.None);
                    Log.Info($"→ {path} → {n.Target}");
                    Engine.Record("move", path, n.Target);
                }
                else
                {
                    await nas!.Delete(path, baseRev, CancellationToken.None);
                    Log.Info($"✕ {path} (moved to the NAS trash)");
                    Engine.Record("delete", path);
                }
            }
            catch (ConflictException) when (n.Target == null)
            {
                // Changed on the NAS since: it stays there, and the changes feed brings the newer copy back here.
                Log.Info($"{n.Path}: changed on the NAS, so it was kept there");
            }
            catch (HttpRequestException e) when (e.StatusCode == HttpStatusCode.NotFound)
            {
                // Not on the NAS (any more): nothing to do there.
            }
            catch (Exception e)
            {
                // shortcut: offline deletes and renames are refused rather than queued; queue them if that gets in the way.
                Log.Error($"{(n.Target != null ? "rename" : "delete")} of {n.Path} on the NAS: {e.Message}");
                status = e is HttpRequestException { StatusCode: null } or IOException
                    ? NTSTATUS.STATUS_CLOUD_FILE_NETWORK_UNAVAILABLE : NTSTATUS.STATUS_CLOUD_FILE_UNSUCCESSFUL;
            }
        }
        try { Ack(n, ack, status); }
        catch (Exception e) { Log.Error($"{n.Path}: answering Windows: {e.Message}"); }
    }

    static unsafe void Ack(Notice n, CF_OPERATION_TYPE type, NTSTATUS status)
    {
        var op = new CF_OPERATION_INFO
        {
            StructSize = (uint)sizeof(CF_OPERATION_INFO),
            Type = type,
            ConnectionKey = n.Connection,
            TransferKey = n.TransferKey,
            RequestKey = n.RequestKey,
        };
        var p = new CF_OPERATION_PARAMETERS();
        long head = (byte*)&p.Anonymous - (byte*)&p;
        if (type == CF_OPERATION_TYPE.CF_OPERATION_TYPE_ACK_DELETE)
        {
            p.ParamSize = (uint)(head + sizeof(CF_OPERATION_PARAMETERS._Anonymous_e__Union._AckDelete_e__Struct));
            p.AckDelete.CompletionStatus = status;
        }
        else
        {
            p.ParamSize = (uint)(head + sizeof(CF_OPERATION_PARAMETERS._Anonymous_e__Union._AckRename_e__Struct));
            p.AckRename.CompletionStatus = status;
        }
        PInvoke.CfExecute(&op, &p).ThrowOnFailure();
    }

    record Fetch(CF_CONNECTION_KEY Connection, long TransferKey, long RequestKey, long Rev, string Path, long FileSize, long Offset, string Process);

    [UnmanagedCallersOnly(CallConvs = [typeof(CallConvStdcall)])]
    static unsafe void OnFetchData(CF_CALLBACK_INFO* info, CF_CALLBACK_PARAMETERS* p)
    {
        try
        {
            var rev = long.Parse(Encoding.UTF8.GetString((byte*)info->FileIdentity, (int)info->FileIdentityLength).Split('\n')[0]);
            var full = info->VolumeDosName.ToString() + info->NormalizedPath.ToString();
            var f = new Fetch(info->ConnectionKey, info->TransferKey, info->RequestKey, rev,
                Rel(full), info->FileSize, p->FetchData.RequiredFileOffset & ~4095L,
                info->ProcessInfo == null ? "?" : Path.GetFileName(info->ProcessInfo->ImagePath.ToString()));
            // The info pointers die when this returns: answer from the thread pool with the copied keys.
            _ = Task.Run(() => Hydrate(f));
        }
        catch (Exception e)
        {
            Log.Error($"fetch callback: {e.Message}");
        }
    }

    [UnmanagedCallersOnly(CallConvs = [typeof(CallConvStdcall)])]
    static unsafe void OnCancelFetchData(CF_CALLBACK_INFO* info, CF_CALLBACK_PARAMETERS* p)
    {
        if (fetches.TryGetValue(info->TransferKey, out var d))
            d.Cancel.Cancel();
    }

    static async Task Hydrate(Fetch f)
    {
        var download = new Download(f.Path, f.FileSize);
        var cts = download.Cancel;
        fetches[f.TransferKey] = download;
        Engine.Refresh();
        var started = Environment.TickCount64;
        long left = f.FileSize - f.Offset;
        int parts = left >= PartMin ? Parts : 1;
        long size = (left / parts + Chunk - 1) / Chunk * Chunk; // whole chunks, so every part starts 4 KB aligned
        try
        {
            // One part failing stops the others: the whole range is then reported as failed.
            using var any = CancellationTokenSource.CreateLinkedTokenSource(cts.Token);
            var tasks = Enumerable.Range(0, parts)
                .Select(i => f.Offset + i * size)
                .Where(from => from < f.FileSize)
                .Select(async from =>
                {
                    try { await Part(f, download, from, Math.Min(from + size, f.FileSize), any.Token); }
                    catch { any.Cancel(); throw; }
                })
                .ToList();
            try
            {
                await Task.WhenAll(tasks);
            }
            catch when (!cts.IsCancellationRequested)
            {
                // Report the first real failure, not the cancellations it caused in the other parts.
                throw tasks.Select(t => t.Exception?.InnerException).FirstOrDefault(x => x is not null and not OperationCanceledException)
                    ?? new IOException("download failed");
            }
            Log.Info($"↓ {f.Path} ({f.FileSize:N0} bytes, {Environment.TickCount64 - started} ms, {tasks.Count} part{(tasks.Count > 1 ? "s" : "")}, for {f.Process})");
            Engine.Record("down", f.Path, size: f.FileSize);
        }
        catch (OperationCanceledException)
        {
            Log.Info($"✕ {f.Path} cancelled");
        }
        catch (Exception e)
        {
            Log.Error($"{f.Path}: {e.Message}");
            var status = e switch
            {
                InvalidDataException => NTSTATUS.STATUS_CLOUD_FILE_NOT_IN_SYNC,
                HttpRequestException or IOException => NTSTATUS.STATUS_CLOUD_FILE_NETWORK_UNAVAILABLE,
                _ => NTSTATUS.STATUS_CLOUD_FILE_UNSUCCESSFUL,
            };
            try { Transfer(f, null, f.Offset, f.FileSize - f.Offset, status); }
            catch (Exception e2) { Log.Error($"{f.Path}: reporting the failure: {e2.Message}"); }
        }
        finally
        {
            fetches.TryRemove(f.TransferKey, out _);
            Engine.Refresh();
        }
    }

    // Bytes [from, to) on their own connection, handed to Windows in 1 MB chunks.
    static async Task Part(Fetch f, Download download, long from, long to, CancellationToken ct)
    {
        using var resp = await nas!.Download(f.Path, from, to < f.FileSize ? to : null, ct);
        // The placeholder describes one revision; bytes of another would splice two versions together.
        // The changes feed updates the placeholder within moments, and the next open succeeds.
        if (resp.Headers.ETag?.Tag != $"\"{f.Rev}\"" || (from > 0 && resp.StatusCode != HttpStatusCode.PartialContent))
            throw new InvalidDataException($"changed on the NAS (now {resp.Headers.ETag?.Tag})");
        await using var body = await resp.Content.ReadAsStreamAsync(ct);
        var buf = ArrayPool<byte>.Shared.Rent(Chunk);
        try
        {
            for (long offset = from; offset < to;)
            {
                int want = (int)Math.Min(Chunk, to - offset);
                int n = await body.ReadAtLeastAsync(buf.AsMemory(0, want), want, throwOnEndOfStream: false, ct);
                if (n < want)
                    throw new IOException("download cut off");
                Transfer(f, buf, offset, n, default);
                offset += n;
                Interlocked.Add(ref download.Done, n);
                Engine.Refresh();
            }
        }
        finally
        {
            ArrayPool<byte>.Shared.Return(buf);
        }
    }

    static unsafe void Transfer(Fetch f, byte[]? buf, long offset, long length, NTSTATUS status)
    {
        var op = new CF_OPERATION_INFO
        {
            StructSize = (uint)sizeof(CF_OPERATION_INFO),
            Type = CF_OPERATION_TYPE.CF_OPERATION_TYPE_TRANSFER_DATA,
            ConnectionKey = f.Connection,
            TransferKey = f.TransferKey,
            RequestKey = f.RequestKey,
        };
        var p = new CF_OPERATION_PARAMETERS();
        // ParamSize is the offset of the union plus the size of the member used (CF_SIZE_OF_OP_PARAM).
        p.ParamSize = (uint)((byte*)&p.Anonymous - (byte*)&p + sizeof(CF_OPERATION_PARAMETERS._Anonymous_e__Union._TransferData_e__Struct));
        fixed (byte* b = buf)
        {
            p.TransferData.CompletionStatus = status;
            p.TransferData.Buffer = b;
            p.TransferData.Offset = offset;
            p.TransferData.Length = length;
            PInvoke.CfExecute(&op, &p).ThrowOnFailure();
        }
    }
}
