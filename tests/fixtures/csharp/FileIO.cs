using System;
using System.IO;

namespace Vmnet.Fixtures
{
    // Fase 3.59 golden fixture: real System.IO.File/Directory/FileStream/
    // FileInfo/DirectoryInfo support, gated by vmnet's own deny-by-default
    // Permissions model (see docs/en/security.md, permissions.go). Every
    // method here does genuine disk I/O against whatever path the Go test
    // passes in — none of it is faked or stubbed, and it all compiles and
    // runs identically against a real CLR.
    public static class FileIO
    {
        public static bool Exists(string path) => File.Exists(path);

        public static string WriteThenReadText(string path, string contents)
        {
            File.WriteAllText(path, contents);
            return File.ReadAllText(path);
        }

        public static int WriteThenReadBytesLength(string path, string contents)
        {
            File.WriteAllText(path, contents);
            byte[] data = File.ReadAllBytes(path);
            return data.Length;
        }

        public static bool DeleteAndCheck(string path)
        {
            File.Delete(path);
            return File.Exists(path);
        }

        public static bool CreateDirectoryAndCheck(string path)
        {
            Directory.CreateDirectory(path);
            return Directory.Exists(path);
        }

        public static string WriteViaFileStreamThenReadViaFile(string path, string contents)
        {
            byte[] bytes = new byte[contents.Length];
            for (int i = 0; i < contents.Length; i++)
            {
                bytes[i] = (byte)contents[i];
            }
            using (FileStream fs = new FileStream(path, FileMode.Create))
            {
                fs.Write(bytes, 0, bytes.Length);
            }
            return File.ReadAllText(path);
        }

        // Exercises both the deny path (UnauthorizedAccessException) and
        // that catch (Exception) alone would also have matched it (Fase
        // 3.59 also fixed a latent bug where several System.IO exception
        // types had no entry in the hand-maintained exception hierarchy at
        // all, so a plain `catch (Exception)` silently failed to match
        // them — see internal/interpreter/typecheck.go's own doc comment).
        public static string ReadCatchingUnauthorized(string path)
        {
            try
            {
                return File.ReadAllText(path);
            }
            catch (UnauthorizedAccessException)
            {
                return "DENIED";
            }
            catch (Exception)
            {
                return "OTHER";
            }
        }

        public static long FileInfoLength(string path)
        {
            FileInfo fi = new FileInfo(path);
            return fi.Length;
        }

        public static bool FileInfoExists(string path)
        {
            FileInfo fi = new FileInfo(path);
            return fi.Exists;
        }

        public static bool DirectoryInfoExists(string path)
        {
            DirectoryInfo di = new DirectoryInfo(path);
            return di.Exists;
        }
    }
}
