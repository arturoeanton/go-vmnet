// Command npoi-demo reads a real legacy .xls file (OLE2/CFBF binary
// format, not the zip-based .xlsx) through the real, unmodified NPOI
// 2.8.0 NuGet package — no dotnet SDK installed, no P/Invoke, no
// third-party .xls parser written for vmnet specifically. It constructs
// a real HSSFWorkbook and drives its instance API directly from Go via
// Assembly.New/Instance.Call (Fase 3.28), the same no-wrapper pattern
// examples/jint-nowrapper uses.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
)

// NPOI's own CellType enum (NPOI.SS.UserModel.CellType) values, as
// returned by ICell.CellType — vmnet has no TypeDef for a BCL/package
// enum to resolve a symbolic name against, so the raw underlying ints
// are matched directly here, same posture every other BCL enum argument
// in this codebase already takes.
const (
	cellTypeNumeric = 0
	cellTypeString  = 1
	cellTypeFormula = 2
	cellTypeBlank   = 3
)

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("NPOI", "2.8.0"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}

	npoiAsm, err := vm.LoadPackage("NPOI")
	if err != nil {
		log.Fatalf("LoadPackage(NPOI): %v", err)
	}

	data, err := os.ReadFile("testdata/sample.xls")
	if err != nil {
		log.Fatalf("reading testdata/sample.xls: %v", err)
	}

	stream, err := npoiAsm.New("System.IO.MemoryStream", vmnet.ByteArray(data))
	if err != nil {
		log.Fatalf("new MemoryStream: %v", err)
	}

	workbook, err := npoiAsm.New("NPOI.HSSF.UserModel.HSSFWorkbook", stream)
	if err != nil {
		log.Fatalf("new HSSFWorkbook: %v", err)
	}

	sheet, err := workbook.Call("GetSheetAt", vmnet.Int32(0))
	if err != nil {
		log.Fatalf("GetSheetAt(0): %v", err)
	}
	sheetInst := sheet.(*vmnet.Instance)

	lastRow, err := sheetInst.Call("get_LastRowNum")
	if err != nil {
		log.Fatalf("get_LastRowNum: %v", err)
	}
	fmt.Printf("Sheet has %d data rows (plus header)\n\n", lastRow.Native())

	rowCount := int(lastRow.Native().(int32))
	for r := 0; r <= rowCount; r++ {
		row, err := sheetInst.Call("GetRow", vmnet.Int32(int32(r)))
		if err != nil {
			log.Fatalf("GetRow(%d): %v", r, err)
		}
		rowInst, ok := row.(*vmnet.Instance)
		if !ok {
			continue
		}

		lastCell, err := rowInst.Call("get_LastCellNum")
		if err != nil {
			log.Fatalf("get_LastCellNum: %v", err)
		}
		cellCount := int(lastCell.Native().(int16))

		for c := 0; c < cellCount; c++ {
			cell, err := rowInst.Call("GetCell", vmnet.Int32(int32(c)))
			if err != nil {
				log.Fatalf("GetCell(%d,%d): %v", r, c, err)
			}
			cellInst, ok := cell.(*vmnet.Instance)
			if !ok {
				fmt.Print("\t")
				continue
			}

			cellType, err := cellInst.Call("get_CellType")
			if err != nil {
				log.Fatalf("get_CellType: %v", err)
			}

			switch cellType.Native().(int32) {
			case cellTypeString:
				v, err := cellInst.Call("get_StringCellValue")
				if err != nil {
					log.Fatalf("get_StringCellValue: %v", err)
				}
				fmt.Printf("%v\t", v.Native())
			case cellTypeNumeric:
				v, err := cellInst.Call("get_NumericCellValue")
				if err != nil {
					log.Fatalf("get_NumericCellValue: %v", err)
				}
				fmt.Printf("%v\t", v.Native())
			case cellTypeFormula:
				v, err := cellInst.Call("get_CellFormula")
				if err != nil {
					log.Fatalf("get_CellFormula: %v", err)
				}
				fmt.Printf("=%v\t", v.Native())
			case cellTypeBlank:
				fmt.Print("\t")
			}
		}
		fmt.Println()
	}
}
