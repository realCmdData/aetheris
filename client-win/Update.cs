using System.Diagnostics;
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text.Json;
using Windows.Win32;

namespace Aetheris;

// Keeps the installed app current from the releases of github.com/realCmdData/aetheris (readable only while the
// repository is public: a private one answers 404 without a token, and none ships in the app): a look a
// minute after start, then every six hours. A newer AetherisSetup.exe downloads next to the exe and must match the
// SHA-256 GitHub lists for it. Once nothing is transferring, the running exe steps aside (Windows lets a running exe
// be renamed, not replaced), the new one takes its name, starts, and waits for this one to quit.
// Trust: whoever can publish releases in that repository can update this app.
static class Update
{
    const string Latest = "https://api.github.com/repos/realCmdData/aetheris/releases/latest";
    const string Asset = "AetherisSetup.exe";

    public static string? Pending { get; private set; } // the version on its way in, for the app window

    public static async Task Run(Profile p)
    {
        if (p.Dev || !Setup.Released || !Setup.Installed(p))
            return; // local builds and copies outside the install folder stay as they are
        await Task.Delay(TimeSpan.FromMinutes(1));
        while (true)
        {
            try { await Check(p); }
            catch (Exception e)
            {
                Pending = null;
                Log.Error($"update: {e.Message}");
            }
            await Task.Delay(TimeSpan.FromHours(6));
        }
    }

    static async Task Check(Profile p)
    {
        using var http = new HttpClient { Timeout = TimeSpan.FromMinutes(15) };
        http.DefaultRequestHeaders.UserAgent.ParseAdd("Aetheris-Updater"); // GitHub's API refuses requests without one
        http.DefaultRequestHeaders.Accept.ParseAdd("application/vnd.github+json");
        var release = await http.GetFromJsonAsync<JsonElement>(Latest);
        if (!Version.TryParse(release.GetProperty("tag_name").GetString()?.TrimStart('v'), out var tag) || Setup.Normal(tag) <= Setup.Version)
            return;
        var asset = release.GetProperty("assets").EnumerateArray().FirstOrDefault(a => a.GetProperty("name").GetString() == Asset);
        if (asset.ValueKind != JsonValueKind.Object || !asset.TryGetProperty("digest", out var d) || d.GetString() is not ['s', 'h', 'a', '2', '5', '6', ':', .. var sha])
            throw new InvalidDataException($"release {tag} has no {Asset} with a SHA-256");

        Pending = $"{tag.Major}.{tag.Minor}.{Math.Max(tag.Build, 0)}";
        Engine.Refresh();
        var exe = Environment.ProcessPath!;
        var dir = Path.GetDirectoryName(exe)!;
        var next = Path.Combine(dir, "Aetheris.new.exe");
        using (var resp = await http.GetAsync(asset.GetProperty("browser_download_url").GetString(), HttpCompletionOption.ResponseHeadersRead))
        {
            resp.EnsureSuccessStatusCode();
            await using var file = File.Create(next);
            await resp.Content.CopyToAsync(file);
        }
        string got;
        await using (var file = File.OpenRead(next))
            got = Convert.ToHexStringLower(await SHA256.HashDataAsync(file));
        if (!got.Equals(sha, StringComparison.OrdinalIgnoreCase))
        {
            File.Delete(next);
            throw new InvalidDataException($"the download of {Pending} doesn't match its SHA-256; not installed");
        }

        while (Local.Current != null || SyncRoot.Downloads > 0)
            await Task.Delay(TimeSpan.FromSeconds(30)); // never in the middle of a transfer
        var old = Path.Combine(dir, "Aetheris.old.exe");
        File.Move(exe, old, overwrite: true);
        try
        {
            File.Move(next, exe);
            Process.Start(new ProcessStartInfo(exe, $"--after {Environment.ProcessId}{(p.Dev ? " --dev" : "")}") { WorkingDirectory = dir })!.Dispose();
        }
        catch
        {
            if (!File.Exists(exe))
                File.Move(old, exe); // this version stays
            throw;
        }
        Log.Info($"updating to {Pending}");
        PInvoke.PostMessage(Tray.Hwnd, PInvoke.WM_CLOSE, 0, 0); // quits as from the menu; the new copy waits for that
    }
}
