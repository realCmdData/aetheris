using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Net.Sockets;
using System.Text.Json;

namespace Aetheris;

record Entry(string Path, bool Dir, long Size, long Mtime, string? Hash, long Rev);

class AuthException(string message) : Exception(message);

// 409: the NAS copy is not the revision we started from. Current is null when it is gone there.
class ConflictException(Entry? current) : Exception("changed on the NAS")
{
    public Entry? Current => current;
}

// The NAS API (docs/protocol.md) over a device token. Never Vercel: LAN first, then the tailnet.
sealed class Nas
{
    static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web);
    static long lanDownUntil;
    public static string? Route { get; private set; } // how the last connection reached the NAS, for the app window

    readonly Profile profile;
    readonly HttpClient http;
    string token;

    public Nas(Profile profile)
    {
        this.profile = profile;
        token = profile.Token ?? throw new AuthException("not paired: run  Aetheris pair <server> <code>");
        http = Client(profile.Server ?? throw new AuthException("not set up: run  Aetheris pair <server> <code>"), profile.Dev ? null : profile.Lan);
    }

    // lan: the NAS's addresses at home, tried before the tailnet; null for the dev server (straight to it).
    // AETHERIS_LAN replaces them (192.0.2.1:8443, unreachable, tests the tailnet route).
    static HttpClient Client(Uri server, string[]? lan)
    {
        var handler = new SocketsHttpHandler { PooledConnectionIdleTimeout = TimeSpan.FromMinutes(1) };
        if (lan != null)
        {
            var home = (Environment.GetEnvironmentVariable("AETHERIS_LAN") is { } test ? [test] : lan).Select(Home).OfType<EndPoint>().ToArray();
            handler.ConnectCallback = (ctx, ct) => ConnectLanFirst(home, ctx, ct);
        }
        else
            Route = "This PC (dev server)";
        return new HttpClient(handler) { BaseAddress = server, Timeout = Timeout.InfiniteTimeSpan };
    }

    // An address at home: "192.168.1.20" (the server's LAN port, 8443), "192.168.1.20:8443" or "nas.local:8443".
    public static EndPoint? Home(string s)
    {
        s = s.Trim();
        if (s == "" || s.Contains('/'))
            return null;
        if (IPEndPoint.TryParse(s, out var ip))
            return ip.Port == 0 ? new IPEndPoint(ip.Address, 8443) : ip;
        int colon = s.LastIndexOf(':');
        return colon > 0 && int.TryParse(s[(colon + 1)..], out var port) && port is > 0 and < 65536 ? new DnsEndPoint(s[..colon], port)
            : colon < 0 ? new DnsEndPoint(s, 8443) : null;
    }

    // The NAS serves its ts.net certificate on the LAN listener too, so TLS verifies the real host name
    // either way; this only picks the TCP route. After a LAN miss, go straight to the tailnet for a minute.
    static async ValueTask<Stream> ConnectLanFirst(EndPoint[] home, SocketsHttpConnectionContext ctx, CancellationToken ct)
    {
        if (home.Length > 0 && Environment.TickCount64 >= Volatile.Read(ref lanDownUntil))
        {
            foreach (var ep in home)
            {
                try
                {
                    var lan = await Dial(ep, TimeSpan.FromSeconds(1.5), ct);
                    Route = "Home network";
                    return lan;
                }
                catch (Exception) when (!ct.IsCancellationRequested) { }
            }
            Volatile.Write(ref lanDownUntil, Environment.TickCount64 + 60_000);
        }
        var tailnet = await Dial(ctx.DnsEndPoint, TimeSpan.FromSeconds(15), ct);
        Route = "Tailscale";
        return tailnet;
    }

    static async Task<Stream> Dial(EndPoint ep, TimeSpan timeout, CancellationToken ct)
    {
        var s = new Socket(SocketType.Stream, ProtocolType.Tcp) { NoDelay = true };
        try
        {
            using var cts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            cts.CancelAfter(timeout);
            await s.ConnectAsync(ep, cts.Token);
            return new NetworkStream(s, ownsSocket: true);
        }
        catch { s.Dispose(); throw; }
    }

    // The device token, plus where the server says it is (its tailnet address, its address at home, the web app).
    public record Claimed(string Token, string? Server, string? Lan, string? Web);

    public static async Task<Claimed> Claim(Uri server, string[]? lan, string code)
    {
        using var http = Client(server, lan);
        using var resp = await http.PostAsJsonAsync("api/v1/devices/claim", new { code, name = Environment.MachineName });
        if (!resp.IsSuccessStatusCode)
            throw new AuthException(resp.StatusCode switch
            {
                HttpStatusCode.Unauthorized => "That code didn't work. Codes last 10 minutes: make a new one in Settings.",
                HttpStatusCode.TooManyRequests => "Too many tries. Wait a minute, then try again.",
                _ => $"Pairing failed ({(int)resp.StatusCode}).",
            });
        var j = await resp.Content.ReadFromJsonAsync<JsonElement>();
        string? Field(string name) => j.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() : null;
        return new(Field("token")!, Field("server"), Field("lan"), Field("web"));
    }

    async Task<HttpResponseMessage> Send(HttpRequestMessage req, CancellationToken ct)
    {
        req.Headers.Authorization = new AuthenticationHeaderValue("Bearer", token);
        var resp = await http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct);
        if (resp.Headers.TryGetValues("X-Aetheris-Token", out var renewed))
            profile.Token = token = renewed.First();
        if (resp.StatusCode == HttpStatusCode.Unauthorized)
            throw new AuthException("this PC was signed out of the NAS: pair it again");
        return resp;
    }

    // The whole tree, parents before children. A stream without its final line was cut off.
    public async Task<(List<Entry> Entries, string Epoch, long Cursor)> Tree(CancellationToken ct = default)
    {
        using var resp = await Send(new(HttpMethod.Get, "api/v1/tree?recursive=1"), ct);
        resp.EnsureSuccessStatusCode();
        using var body = new StreamReader(await resp.Content.ReadAsStreamAsync(ct));
        var entries = new List<Entry>();
        while (await body.ReadLineAsync(ct) is { } line)
        {
            using var doc = JsonDocument.Parse(line);
            if (doc.RootElement.TryGetProperty("done", out _))
                return (entries, doc.RootElement.GetProperty("epoch").GetString()!, doc.RootElement.GetProperty("cursor").GetInt64());
            entries.Add(doc.RootElement.Deserialize<Entry>(Json)!);
        }
        throw new IOException("tree listing was cut off");
    }

    public record ChangePage(string Epoch, long Cursor, List<Change> Changes);

    // Long-polls up to wait seconds. null: the index was rebuilt (410), resync from the tree.
    public async Task<ChangePage?> Changes(string epoch, long cursor, int wait, CancellationToken ct)
    {
        using var cts = CancellationTokenSource.CreateLinkedTokenSource(ct);
        cts.CancelAfter(TimeSpan.FromSeconds(wait + 20)); // a PC that slept can leave a dead connection behind
        using var resp = await Send(new(HttpMethod.Get, $"api/v1/changes?epoch={Uri.EscapeDataString(epoch)}&cursor={cursor}&wait={wait}"), cts.Token);
        if (resp.StatusCode == HttpStatusCode.Gone)
            return null;
        resp.EnsureSuccessStatusCode();
        return await resp.Content.ReadFromJsonAsync<ChangePage>(Json, cts.Token);
    }

    async Task<T> Post<T>(string url, object body, CancellationToken ct)
    {
        using var resp = await Send(new(HttpMethod.Post, url) { Content = JsonContent.Create(body, options: Json) }, ct);
        if (resp.StatusCode == HttpStatusCode.Conflict)
        {
            var err = await resp.Content.ReadFromJsonAsync<JsonElement>(ct);
            if (err.TryGetProperty("current", out var cur))
                throw new ConflictException(cur.ValueKind == JsonValueKind.Null ? null : cur.Deserialize<Entry>(Json));
            throw new HttpRequestException($"{url}: {err.GetProperty("error").GetString()}", null, resp.StatusCode);
        }
        if (!resp.IsSuccessStatusCode)
            throw new HttpRequestException($"{url}: {(int)resp.StatusCode} {await resp.Content.ReadAsStringAsync(ct)}", null, resp.StatusCode);
        return resp.StatusCode == HttpStatusCode.NoContent ? default! : (await resp.Content.ReadFromJsonAsync<T>(Json, ct))!;
    }

    record UploadStart(string Id, int ChunkSize);

    // Sends the bytes and returns the upload id. Commit places it; after a conflict it can be committed again elsewhere.
    // shortcut: a dropped connection restarts the file from zero; resume via GET upload/{id} once big files over the tailnet matter.
    public async Task<string> Upload(Stream data, long length, CancellationToken ct, Action<long>? sent = null)
    {
        var up = await Post<UploadStart>("api/v1/upload", new { }, ct);
        var buf = new byte[Math.Clamp(length, 1, up.ChunkSize)];
        long offset = 0;
        int n;
        while ((n = await data.ReadAtLeastAsync(buf, buf.Length, throwOnEndOfStream: false, ct)) > 0)
        {
            var req = new HttpRequestMessage(HttpMethod.Put, $"api/v1/upload/{up.Id}?offset={offset}") { Content = new ByteArrayContent(buf, 0, n) };
            using var resp = await Send(req, ct);
            if (!resp.IsSuccessStatusCode)
                throw new HttpRequestException($"upload chunk at {offset}: {(int)resp.StatusCode}", null, resp.StatusCode);
            offset += n;
            sent?.Invoke(offset);
        }
        return up.Id;
    }

    // created: the file's creation time on this PC (unix ms), which the NAS keeps as the file's own.
    public Task<Entry> Commit(string id, string path, long baseRev, long mtime, long created, CancellationToken ct) =>
        Post<Entry>($"api/v1/upload/{id}/commit", new { path, baseRev, mtime, created }, ct);

    // A folder's own icon (PNG), shown for it on the web.
    public async Task SetIcon(string path, byte[] png, CancellationToken ct)
    {
        using var resp = await Send(new(HttpMethod.Put, "api/v1/icon?path=" + Uri.EscapeDataString(path)) { Content = new ByteArrayContent(png) }, ct);
        resp.EnsureSuccessStatusCode();
    }

    public Task Logout() => Post<object?>("api/v1/auth/logout", new { }, CancellationToken.None);

    public Task<Entry> Mkdir(string path, CancellationToken ct) => Post<Entry>("api/v1/mkdir", new { path }, ct);

    public Task<Entry> Move(string from, string to, long baseRev, CancellationToken ct) => Post<Entry>("api/v1/move", new { from, to, baseRev }, ct);

    public Task Delete(string path, long baseRev, CancellationToken ct) => Post<object?>("api/v1/delete", new { path, baseRev }, ct);

    // Bytes [from, to) of the file; null to = to its end. Callers check the ETag: it must be the rev they expect.
    public async Task<HttpResponseMessage> Download(string path, long from, long? to, CancellationToken ct)
    {
        var req = new HttpRequestMessage(HttpMethod.Get, "api/v1/file?path=" + Uri.EscapeDataString(path));
        if (from > 0 || to != null)
            req.Headers.Range = new RangeHeaderValue(from, to - 1);
        var resp = await Send(req, ct);
        if (!resp.IsSuccessStatusCode)
        {
            resp.Dispose();
            throw new HttpRequestException($"{path}: {(int)resp.StatusCode}", null, resp.StatusCode);
        }
        return resp;
    }
}
