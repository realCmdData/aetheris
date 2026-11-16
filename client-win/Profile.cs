using System.ComponentModel;
using Microsoft.Win32;
using Windows.Win32;
using Windows.Win32.Foundation;
using Windows.Win32.Security.Cryptography;

namespace Aetheris;

// Everything follows from the profile: "nas" is your NAS, "dev" the loopback dev server.
// The device token is stored with DPAPI (current Windows user), next to the log.
sealed class Profile(bool dev)
{
    public bool Dev => dev;
    public string Name => dev ? "dev" : "nas";
    public string DisplayName => dev ? "Aetheris Dev" : "Aetheris";
    public string Root => Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), DisplayName);
    // The taskbar and Start menu identity of the app window and its shortcuts.
    public string AppId => dev ? "Aetheris.Dev" : "Aetheris";

    // Where the NAS is, from the setup and the server's answer to pairing (server.txt, key=value lines):
    // server = its tailnet address (https://aetheris.<tailnet>.ts.net), lan = host:port at home (tried first, the
    // same certificate), web = the web app's address. The dev profile always uses the loopback dev server.
    string ConnectionFile => Path.Combine(Dir, "server.txt");

    Dictionary<string, string> Connection => File.Exists(ConnectionFile)
        ? File.ReadAllLines(ConnectionFile).Select(l => l.Split('=', 2)).Where(kv => kv.Length == 2).ToDictionary(kv => kv[0].Trim(), kv => kv[1].Trim())
        : [];

    public Uri? Server => dev ? new("http://127.0.0.1:8080") : Connection.GetValueOrDefault("server") is { Length: > 0 } s ? new(s) : null;
    public string? Web => dev ? "http://localhost:5173" : Connection.GetValueOrDefault("web") is { Length: > 0 } w ? w : null;
    public string[] Lan => dev ? [] : (Connection.GetValueOrDefault("lan") ?? "").Split(',', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries);

    public void SaveConnection(Uri server, string[] lan, string? web)
    {
        if (dev)
            return;
        Directory.CreateDirectory(Dir);
        File.WriteAllLines(ConnectionFile + ".tmp", [$"server={server.GetLeftPart(UriPartial.Authority)}", $"lan={string.Join(',', lan)}", $"web={web}"]);
        File.Move(ConnectionFile + ".tmp", ConnectionFile, overwrite: true);
    }

    public string Dir => Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Aetheris", Name);
    string TokenFile => Path.Combine(Dir, "token.bin");
    public string LogFile => Path.Combine(Dir, "aetheris.log");
    // Per user, no admin rights: where the released exe installs itself (Setup.cs).
    public string InstallDir => Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Programs", DisplayName);

    // The Aetheris folder itself, then the folders from elsewhere on this PC that sync too.
    public Folder Main => new(Root, "");
    public IEnumerable<Folder> All => Folders.Prepend(Main);

    // Folders from elsewhere on this PC that sync too, one "local path<TAB>NAS path" per line. The NAS path is
    // fixed when the folder is added, so renaming the PC later doesn't start a second copy.
    string FoldersFile => Path.Combine(Dir, "folders.txt");

    public List<Folder> Folders
    {
        get => File.Exists(FoldersFile)
            ? File.ReadAllLines(FoldersFile).Select(l => l.Split('\t')).Where(f => f.Length == 2).Select(f => new Folder(f[0], f[1])).ToList()
            : [];
        set
        {
            Directory.CreateDirectory(Dir);
            File.WriteAllLines(FoldersFile + ".tmp", value.Select(f => $"{f.Local}\t{f.Nas}"));
            File.Move(FoldersFile + ".tmp", FoldersFile, overwrite: true);
        }
    }

    const string RunKey = @"Software\Microsoft\Windows\CurrentVersion\Run";

    // Start with Windows: a per-user Run entry that points at this exe.
    public bool Autostart
    {
        get
        {
            using var k = Registry.CurrentUser.OpenSubKey(RunKey);
            return k?.GetValue(DisplayName) != null;
        }
        set
        {
            using var k = Registry.CurrentUser.CreateSubKey(RunKey);
            if (value)
                k.SetValue(DisplayName, $"\"{Environment.ProcessPath}\"" + (dev ? " --dev" : ""));
            else
                k.DeleteValue(DisplayName, throwOnMissingValue: false);
        }
    }

    public string? Token
    {
        get => File.Exists(TokenFile) ? System.Text.Encoding.UTF8.GetString(Dpapi(File.ReadAllBytes(TokenFile), protect: false)) : null;
        set
        {
            Directory.CreateDirectory(Path.GetDirectoryName(TokenFile)!);
            var tmp = TokenFile + ".tmp";
            File.WriteAllBytes(tmp, Dpapi(System.Text.Encoding.UTF8.GetBytes(value!), protect: true));
            File.Move(tmp, TokenFile, overwrite: true);
        }
    }

    public void ForgetToken() => File.Delete(TokenFile);

    // Free up space automatically: files not opened for this many days become online-only. 0 = off.
    string AutoFreeFile => Path.Combine(Dir, "autofree.txt");

    public int AutoFreeDays
    {
        get => File.Exists(AutoFreeFile) && int.TryParse(File.ReadAllText(AutoFreeFile).Trim(), out var d) && d > 0 ? d : 0;
        set
        {
            Directory.CreateDirectory(Dir);
            File.WriteAllText(AutoFreeFile, value.ToString());
        }
    }

    // How far each folder had caught up with the NAS: an item that existed then but is missing now was
    // deleted or renamed here while the app wasn't running.
    string SyncedFile(Folder f) => Path.Combine(Dir, f.Backup ? $"synced-{f.Id}.txt" : "synced.txt");
    string DriveFile => Path.Combine(Dir, "drive.txt");

    // The drive letter the folder also shows up as: "A".."Z", "off", or null before the first start.
    public string? Drive
    {
        get => File.Exists(DriveFile) && File.ReadAllText(DriveFile).Trim() is { Length: > 0 } v ? v : null;
        set
        {
            Directory.CreateDirectory(Dir);
            if (value == null)
                File.Delete(DriveFile);
            else
                File.WriteAllText(DriveFile, value);
        }
    }

    public void ForgetSynced(Folder f) => File.Delete(SyncedFile(f));

    // How the folder was last registered with Windows: "packaged" (with the app's package identity) or "unpackaged".
    string RegisteredFile => Path.Combine(Dir, "registered.txt");

    public string? RegisteredAs
    {
        get => File.Exists(RegisteredFile) ? File.ReadAllText(RegisteredFile).Trim() : null;
        set
        {
            Directory.CreateDirectory(Dir);
            if (value == null)
                File.Delete(RegisteredFile);
            else
                File.WriteAllText(RegisteredFile, value);
        }
    }

    public (string Epoch, long Cursor)? Synced(Folder f) =>
        File.Exists(SyncedFile(f)) && File.ReadAllText(SyncedFile(f)).Split(' ') is [var epoch, var c] && long.TryParse(c, out var cursor)
            ? (epoch, cursor) : null;

    public void SetSynced(Folder f, string epoch, long cursor)
    {
        Directory.CreateDirectory(Dir);
        File.WriteAllText(SyncedFile(f) + ".tmp", $"{epoch} {cursor}");
        File.Move(SyncedFile(f) + ".tmp", SyncedFile(f), overwrite: true);
    }

    static unsafe byte[] Dpapi(byte[] data, bool protect)
    {
        fixed (byte* p = data)
        {
            var input = new CRYPT_INTEGER_BLOB { cbData = (uint)data.Length, pbData = p };
            CRYPT_INTEGER_BLOB output;
            BOOL ok = protect
                ? PInvoke.CryptProtectData(&input, default, null, null, null, PInvoke.CRYPTPROTECT_UI_FORBIDDEN, &output)
                : PInvoke.CryptUnprotectData(&input, null, null, null, null, PInvoke.CRYPTPROTECT_UI_FORBIDDEN, &output);
            if (!ok) throw new Win32Exception();
            try { return new Span<byte>(output.pbData, (int)output.cbData).ToArray(); }
            finally { PInvoke.LocalFree((HLOCAL)output.pbData); }
        }
    }
}
