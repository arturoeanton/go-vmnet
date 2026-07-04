// Regenerates ../testdata/sample.xlsx — a real .xlsx (OOXML/zip binary
// format) written by the real ClosedXML 0.105.0 package via the
// official .NET SDK. Dev-only: needed only to *produce* the fixture,
// never to *read* it — examples/closedxml-demo/main.go reads it back
// with vmnet, no dotnet installed required at runtime.
//
//   dotnet run --project examples/closedxml-demo/generate
//   cp examples/closedxml-demo/generate/sample.xlsx examples/closedxml-demo/testdata/
using ClosedXML.Excel;

using var wb = new XLWorkbook();
var ws = wb.Worksheets.Add("Sales");

ws.Cell(1, 1).Value = "Product";
ws.Cell(1, 2).Value = "Units";
ws.Cell(1, 3).Value = "Price";

string[] products = { "Widget", "Gadget", "Gizmo" };
int[] units = { 12, 7, 25 };
double[] prices = { 9.99, 19.5, 3.25 };

for (int i = 0; i < products.Length; i++)
{
    ws.Cell(i + 2, 1).Value = products[i];
    ws.Cell(i + 2, 2).Value = units[i];
    ws.Cell(i + 2, 3).Value = prices[i];
}

ws.Cell(products.Length + 2, 1).Value = "Total";
ws.Cell(products.Length + 2, 2).FormulaA1 = "SUM(B2:B4)";

wb.SaveAs("sample.xlsx");
Console.WriteLine("wrote sample.xlsx");
