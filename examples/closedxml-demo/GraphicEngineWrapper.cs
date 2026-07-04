// A minimal, real compiled C# wrapper — not because ClosedXML's own
// instance API can't be driven directly from Go (examples/npoi-demo
// proves that pattern for NPOI), but because ClosedXML's real
// IXLGraphicEngine is an INTERFACE: satisfying it needs a concrete
// backing type with real methods, which vmnet has no way to fabricate
// purely from Go. ClosedXML's own DefaultGraphicEngine implementation
// depends on SixLabors.Fonts/System.Memory internals that hit a real,
// deep vmnet limitation unrelated to reading cell data at all (generic
// type-parameter substitution for typeof(T) inside a generic class's
// own static field initializers — see docs/en/ROADMAP.md Fase 3.40).
// NullGraphicEngine sidesteps that entirely: real cell VALUES never
// depend on font metrics, only auto-column-width calculation does.
using System;
using System.IO;
using ClosedXML.Excel;
using ClosedXML.Excel.Drawings;
using ClosedXML.Graphics;

namespace VmnetClosedXmlDemo
{
	public class NullGraphicEngine : IXLGraphicEngine
	{
		public XLPictureInfo GetPictureInfo(Stream imageStream, XLPictureFormat expectedFormat)
		{
			throw new NotSupportedException("NullGraphicEngine does not support picture metrics");
		}

		public double GetTextHeight(IXLFontBase font, double dpiY)
		{
			return font.FontSize * dpiY / 72.0;
		}

		public double GetTextWidth(string text, IXLFontBase font, double dpiX)
		{
			return text.Length * font.FontSize * dpiX / 144.0;
		}

		public double GetMaxDigitWidth(IXLFontBase font, double dpiX)
		{
			return font.FontSize * dpiX / 144.0;
		}

		public double GetDescent(IXLFontBase font, double dpiY)
		{
			return font.FontSize * dpiY / 288.0;
		}

		public GlyphBox GetGlyphBox(ReadOnlySpan<int> graphemeCluster, IXLFontBase font, Dpi dpi)
		{
			float width = (float)(font.FontSize * dpi.X / 144.0);
			return new GlyphBox(width, (float)font.FontSize, (float)(font.FontSize / 4.0));
		}
	}

	public static class WorkbookOpener
	{
		private static bool s_engineSet;

		// Sets the process-wide default graphic engine once (its own
		// getter is internal to ClosedXML, so a null-check from outside
		// isn't possible — a local flag stands in for it instead), then
		// opens a real XLWorkbook from a real .xlsx byte stream — the
		// returned instance is driven directly from Go afterward
		// (Assembly.New/Instance.Call, Fase 3.28), exactly like NPOI's
		// own instance API.
		public static XLWorkbook OpenWorkbook(Stream stream)
		{
			if (!s_engineSet)
			{
				LoadOptions.DefaultGraphicEngine = new NullGraphicEngine();
				s_engineSet = true;
			}
			return new XLWorkbook(stream);
		}
	}
}
