// Command closedxml-demo reads a real .xlsx file (OOXML/zip binary
// format) through the real, unmodified ClosedXML 0.105.0 NuGet package —
// no dotnet SDK installed at runtime, no third-party .xlsx parser
// written for vmnet specifically. A tiny compiled C# wrapper
// (GraphicEngineWrapper.dll, built from GraphicEngineWrapper.cs in this
// directory) supplies a minimal IXLGraphicEngine implementation and
// constructs the real XLWorkbook — needed only because ClosedXML's own
// DefaultGraphicEngine depends on SixLabors.Fonts/System.Memory
// internals that hit a real, deep vmnet limitation unrelated to reading
// cell data at all (generic type-parameter substitution for typeof(T)
// inside a generic class's own static field initializers — see
// docs/en/ROADMAP.md Fase 3.40). Once constructed, the workbook's own
// instance API is driven directly from Go via Assembly.New/Instance.Call
// (Fase 3.28), the same no-wrapper pattern examples/jint-nowrapper uses
// for everything past that one construction step.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
)

// ClosedXML's own XLDataType enum (ClosedXML.Excel.XLDataType) values,
// as returned by IXLCell.DataType — vmnet has no TypeDef for a BCL/
// package enum to resolve a symbolic name against, so the raw
// underlying ints are matched directly here, same posture
// examples/npoi-demo already takes for NPOI.SS.UserModel.CellType.
const (
	dataTypeBlank    = 0
	dataTypeBoolean  = 1
	dataTypeNumber   = 2
	dataTypeText     = 3
	dataTypeError    = 4
	dataTypeDateTime = 5
	dataTypeTimeSpan = 6
)

func main() {
	vm := vmnet.New()
	// AllowFileWrite (Fase 3.83): ClosedXML's own real internals call
	// System.IO.Path.GetTempFileName() while parsing the workbook's zip
	// parts — previously never reached at all (masked by the List<T>
	// (IEnumerable<T>) construction bug this same Fase fixed elsewhere
	// silently short-circuiting an earlier real code path with an
	// always-empty list). A real, empty 0-byte temp file, same as every
	// other real Path.GetTempFileName() call in this codebase.
	vm.Permissions().AllowFileWrite = true

	if err := vm.NuGet().Add("ClosedXML", "0.105.0"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}

	closedXmlAsm, err := vm.LoadPackage("ClosedXML")
	if err != nil {
		log.Fatalf("LoadPackage(ClosedXML): %v", err)
	}

	data, err := os.ReadFile("testdata/sample.xlsx")
	if err != nil {
		log.Fatalf("reading testdata/sample.xlsx: %v", err)
	}

	stream, err := closedXmlAsm.New("System.IO.MemoryStream", vmnet.ByteArray(data))
	if err != nil {
		log.Fatalf("new MemoryStream: %v", err)
	}

	wrapperData, err := os.ReadFile("bin/Release/netstandard2.0/GraphicEngineWrapper.dll")
	if err != nil {
		log.Fatalf("read GraphicEngineWrapper.dll: %v (run `dotnet build GraphicEngineWrapper.csproj -c Release` in this directory first)", err)
	}
	wrapperAsm, err := vm.LoadBytes("GraphicEngineWrapper.dll", wrapperData)
	if err != nil {
		log.Fatalf("LoadBytes(GraphicEngineWrapper.dll): %v", err)
	}
	wrapperAsm.WithDependencies(closedXmlAsm)

	workbookVal, err := wrapperAsm.Call("VmnetClosedXmlDemo.WorkbookOpener", "OpenWorkbook", stream)
	if err != nil {
		log.Fatalf("OpenWorkbook: %v", err)
	}
	workbook := workbookVal.(*vmnet.Instance)

	sheet, err := workbook.Call("Worksheet", vmnet.Int32(1))
	if err != nil {
		log.Fatalf("Worksheet(1): %v", err)
	}
	sheetInst := sheet.(*vmnet.Instance)

	lastRow, err := sheetInst.Call("LastRowUsed")
	if err != nil {
		log.Fatalf("LastRowUsed: %v", err)
	}
	lastRowInst := lastRow.(*vmnet.Instance)
	rowNum, err := lastRowInst.Call("RowNumber")
	if err != nil {
		log.Fatalf("RowNumber: %v", err)
	}

	lastCol, err := sheetInst.Call("LastColumnUsed")
	if err != nil {
		log.Fatalf("LastColumnUsed: %v", err)
	}
	lastColInst := lastCol.(*vmnet.Instance)
	colNum, err := lastColInst.Call("ColumnNumber")
	if err != nil {
		log.Fatalf("ColumnNumber: %v", err)
	}

	rows := int(rowNum.Native().(int32))
	cols := int(colNum.Native().(int32))
	fmt.Printf("Sheet has %d rows, %d columns\n\n", rows, cols)

	for r := 1; r <= rows; r++ {
		for c := 1; c <= cols; c++ {
			cell, err := sheetInst.Call("Cell", vmnet.Int32(int32(r)), vmnet.Int32(int32(c)))
			if err != nil {
				log.Fatalf("Cell(%d,%d): %v", r, c, err)
			}
			cellInst := cell.(*vmnet.Instance)

			dataType, err := cellInst.Call("get_DataType")
			if err != nil {
				log.Fatalf("get_DataType: %v", err)
			}

			hasFormula, err := cellInst.Call("get_HasFormula")
			if err != nil {
				log.Fatalf("get_HasFormula: %v", err)
			}
			// vmnet has no distinct bool Value kind — a C# bool collapses
			// to the same int32 representation every other CIL-primitive
			// bool does (see runtime.Kind's doc comment), so Native()
			// here is int32(0)/int32(1), never a Go bool.
			if hasFormula.Native().(int32) != 0 {
				v, err := cellInst.Call("get_FormulaA1")
				if err != nil {
					log.Fatalf("get_FormulaA1: %v", err)
				}
				fmt.Printf("=%v\t", v.Native())
				continue
			}

			switch dataType.Native().(int32) {
			case dataTypeText:
				v, err := cellInst.Call("GetString")
				if err != nil {
					log.Fatalf("GetString: %v", err)
				}
				fmt.Printf("%v\t", v.Native())
			case dataTypeNumber:
				v, err := cellInst.Call("GetDouble")
				if err != nil {
					log.Fatalf("GetDouble: %v", err)
				}
				fmt.Printf("%v\t", v.Native())
			case dataTypeBlank:
				fmt.Print("\t")
			default:
				fmt.Print("?\t")
			}
		}
		fmt.Println()
	}
}
