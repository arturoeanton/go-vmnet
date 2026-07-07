using System.Net.Http;
using System.Threading.Tasks;

namespace Vmnet.Fixtures
{
    // Fase 3.82 golden fixture: the exact real shape ClosedXML's own
    // netstandard2.0 PolyfillExtensions shim uses to implement
    // HttpClient.GetStringAsync (not natively available pre-.NET
    // Standard 2.1) — GetAsync, EnsureSuccessStatusCode, then
    // Content.ReadAsStringAsync — plus the two sibling Read overloads it
    // also implements the same way. Each async method has a plain
    // synchronous `RunXxx` entry point that unwraps its Task via
    // GetAwaiter().GetResult() (same convention as Async.cs's own
    // AsyncTest fixture) — asm.Call on an async method directly would
    // return the Task object itself, not its unwrapped result.
    public static class HttpClientTest
    {
        public static async Task<string> GetStringViaGetAsync(string url)
        {
            using (var client = new HttpClient())
            using (var response = await client.GetAsync(url))
            {
                response.EnsureSuccessStatusCode();
                return await response.Content.ReadAsStringAsync();
            }
        }

        public static string RunGetString(string url)
        {
            return GetStringViaGetAsync(url).GetAwaiter().GetResult();
        }

        public static async Task<int> GetByteCountViaGetAsync(string url)
        {
            using (var client = new HttpClient())
            using (var response = await client.GetAsync(url))
            {
                response.EnsureSuccessStatusCode();
                byte[] bytes = await response.Content.ReadAsByteArrayAsync();
                return bytes.Length;
            }
        }

        public static int RunGetByteCount(string url)
        {
            return GetByteCountViaGetAsync(url).GetAwaiter().GetResult();
        }

        public static async Task<bool> IsSuccessViaGetAsync(string url)
        {
            using (var client = new HttpClient())
            using (var response = await client.GetAsync(url))
            {
                return response.IsSuccessStatusCode;
            }
        }

        public static bool RunIsSuccess(string url)
        {
            return IsSuccessViaGetAsync(url).GetAwaiter().GetResult();
        }

        public static async Task<string> EnsureSuccessThrowsOn404(string url)
        {
            using (var client = new HttpClient())
            using (var response = await client.GetAsync(url))
            {
                response.EnsureSuccessStatusCode();
                return await response.Content.ReadAsStringAsync();
            }
        }

        public static string RunEnsureSuccessThrowsOn404(string url)
        {
            return EnsureSuccessThrowsOn404(url).GetAwaiter().GetResult();
        }
    }
}
