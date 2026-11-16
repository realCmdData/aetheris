using System.Runtime.InteropServices;
using Windows.Graphics.Imaging;
using Windows.Storage.Streams;
using Windows.Win32;
using Windows.Win32.Graphics.Gdi;
using Windows.Win32.UI.WindowsAndMessaging;

namespace Aetheris;

// A synced folder's own icon: Downloads' arrow, Pictures' landscape. Windows keeps it in the folder's desktop.ini;
// Explorer keeps showing it (the folder's registration names it, SyncRoot.Register), and the NAS gets a PNG of it so
// the web shows it for that folder under Computers. Plain folders have none and keep the usual folder icon.
static class FolderIcon
{
    const int Size = 96; // drawn at 24–88 px on the web, so it stays sharp on high-density screens
    public const string Plain = @"%SystemRoot%\System32\imageres.dll,-3"; // Windows' folder

    static readonly HashSet<string> sent = [];

    // "file,index" from desktop.ini (IconResource, or the older IconFile + IconIndex); null when the folder has none.
    public static string? Own(string folder)
    {
        var ini = Path.Combine(folder, "desktop.ini");
        if (!File.Exists(ini))
            return null;
        string? resource = null, file = null, index = "0";
        foreach (var line in File.ReadAllLines(ini))
        {
            var kv = line.Split('=', 2, StringSplitOptions.TrimEntries);
            if (kv.Length != 2)
                continue;
            if (kv[0].Equals("IconResource", StringComparison.OrdinalIgnoreCase))
                resource = kv[1];
            else if (kv[0].Equals("IconFile", StringComparison.OrdinalIgnoreCase))
                file = kv[1];
            else if (kv[0].Equals("IconIndex", StringComparison.OrdinalIgnoreCase))
                index = kv[1];
        }
        resource ??= file != null ? $"{file},{index}" : null;
        if (resource == null)
            return null;
        int comma = resource.LastIndexOf(',');
        var path = Environment.ExpandEnvironmentVariables(comma > 0 ? resource[..comma] : resource).Trim('"');
        if (!Path.IsPathRooted(path))
            path = Path.Combine(folder, path);
        return $"{path},{(comma > 0 ? resource[(comma + 1)..].Trim() : "0")}";
    }

    public static async Task Send(Nas nas, Folder f, CancellationToken ct)
    {
        if (Own(f.Local) is not { } resource)
            return;
        lock (sent)
            if (!sent.Add(f.Nas))
                return;
        try
        {
            var (pixels, w, h) = Pixels(resource);
            await nas.SetIcon(f.Nas, await Png(pixels, w, h), ct);
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            lock (sent)
                sent.Remove(f.Nas); // next time
            Log.Error($"icon of {f.Local}: {e.Message}");
        }
    }

    // The icon's 32-bit colour image (straight alpha), as BGRA rows from the top.
    static unsafe (byte[] Pixels, int Width, int Height) Pixels(string resource)
    {
        int comma = resource.LastIndexOf(',');
        HICON icon;
        fixed (char* file = resource[..comma])
            PInvoke.SHDefExtractIcon(file, int.Parse(resource[(comma + 1)..]), 0, &icon, null, Size).ThrowOnFailure();
        if (icon == default)
            throw new FileNotFoundException($"no icon in {resource}");
        try
        {
            ICONINFO parts;
            if (!PInvoke.GetIconInfo(icon, &parts))
                throw new COMException("GetIconInfo failed");
            try
            {
                BITMAP info;
                PInvoke.GetObject(parts.hbmColor, sizeof(BITMAP), &info);
                int w = info.bmWidth, h = Math.Abs(info.bmHeight);
                var pixels = new byte[w * h * 4];
                var header = new BITMAPINFO();
                header.bmiHeader.biSize = (uint)sizeof(BITMAPINFOHEADER);
                header.bmiHeader.biWidth = w;
                header.bmiHeader.biHeight = -h; // top-down
                header.bmiHeader.biPlanes = 1;
                header.bmiHeader.biBitCount = 32;
                var dc = PInvoke.GetDC(default);
                try
                {
                    fixed (byte* p = pixels)
                        if (PInvoke.GetDIBits(dc, parts.hbmColor, 0, (uint)h, p, &header, DIB_USAGE.DIB_RGB_COLORS) == 0)
                            throw new COMException("GetDIBits failed");
                }
                finally
                {
                    PInvoke.ReleaseDC(default, dc);
                }
                return (pixels, w, h);
            }
            finally
            {
                PInvoke.DeleteObject(parts.hbmColor);
                PInvoke.DeleteObject(parts.hbmMask);
            }
        }
        finally
        {
            PInvoke.DestroyIcon(icon);
        }
    }

    static async Task<byte[]> Png(byte[] pixels, int w, int h)
    {
        using var stream = new InMemoryRandomAccessStream();
        var encoder = await BitmapEncoder.CreateAsync(BitmapEncoder.PngEncoderId, stream);
        encoder.SetPixelData(BitmapPixelFormat.Bgra8, BitmapAlphaMode.Straight, (uint)w, (uint)h, 96, 96, pixels); // icons keep straight alpha
        await encoder.FlushAsync();
        var png = new byte[stream.Size];
        stream.Seek(0);
        using var reader = new DataReader(stream);
        await reader.LoadAsync((uint)stream.Size);
        reader.ReadBytes(png);
        return png;
    }
}
