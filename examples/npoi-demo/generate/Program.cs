// Regenerates ../testdata/sample.xls — a real legacy .xls (OLE2/CFBF
// binary format, not the zip-based .xlsx) written by the real NPOI
// 2.8.0 package via the official .NET SDK. Dev-only: needed only to
// *produce* the fixture, never to *read* it — examples/npoi-demo/main.go
// reads it back with vmnet, no dotnet installed required at runtime.
//
//   dotnet run --project examples/npoi-demo/generate
//   cp examples/npoi-demo/generate/sample.xls examples/npoi-demo/testdata/
using NPOI.HSSF.UserModel;
using NPOI.SS.UserModel;

var wb = new HSSFWorkbook();
var sheet = wb.CreateSheet("Sales");

var header = sheet.CreateRow(0);
header.CreateCell(0).SetCellValue("Product");
header.CreateCell(1).SetCellValue("Units");
header.CreateCell(2).SetCellValue("Price");

string[] products = { "Widget", "Gadget", "Gizmo" };
int[] units = { 12, 7, 25 };
double[] prices = { 9.99, 19.5, 3.25 };

for (int i = 0; i < products.Length; i++)
{
    var row = sheet.CreateRow(i + 1);
    row.CreateCell(0).SetCellValue(products[i]);
    row.CreateCell(1).SetCellValue(units[i]);
    row.CreateCell(2).SetCellValue(prices[i]);
}

var totalRow = sheet.CreateRow(products.Length + 1);
totalRow.CreateCell(0).SetCellValue("Total");
var totalCell = totalRow.CreateCell(1);
totalCell.SetCellFormula("SUM(B2:B4)");

using var fs = new FileStream("sample.xls", FileMode.Create, FileAccess.Write);
wb.Write(fs);
Console.WriteLine("wrote sample.xls");
