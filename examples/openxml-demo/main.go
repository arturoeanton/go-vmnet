// Command openxml-demo generates a real .docx file (OOXML/zip binary
// format) through the real, unmodified DocumentFormat.OpenXml 3.1.1
// NuGet package — no dotnet SDK installed at runtime, no third-party
// .docx writer written for vmnet specifically. It drives
// WordprocessingDocument's real static Create factory plus the
// Document/Body/Paragraph/Run/Text element tree directly from Go via
// Assembly.Call/Assembly.New/Instance.Call (Fase 3.28), the same
// no-wrapper pattern examples/jint-nowrapper and examples/npoi-demo use
// — unlike examples/closedxml-demo, generating a document never touches
// ClosedXML's own IXLGraphicEngine abstraction, so no compiled C#
// wrapper is needed here at all.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
)

// DocumentFormat.OpenXml.WordprocessingDocumentType enum value for a
// plain .docx (as opposed to .dotx/.docm/.dotm) — vmnet has no TypeDef
// for a BCL/package enum to resolve a symbolic name against, so the raw
// underlying int is matched directly, same posture
// examples/npoi-demo/examples/closedxml-demo already take for their own
// package enums.
const wordprocessingDocumentTypeDocument = 0

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("DocumentFormat.OpenXml", "3.1.1"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}

	openXmlAsm, err := vm.LoadPackage("DocumentFormat.OpenXml")
	if err != nil {
		log.Fatalf("LoadPackage(DocumentFormat.OpenXml): %v", err)
	}

	stream, err := openXmlAsm.New("System.IO.MemoryStream")
	if err != nil {
		log.Fatalf("new MemoryStream: %v", err)
	}

	docVal, err := openXmlAsm.Call("DocumentFormat.OpenXml.Packaging.WordprocessingDocument", "Create", stream, vmnet.Int32(wordprocessingDocumentTypeDocument))
	if err != nil {
		log.Fatalf("WordprocessingDocument.Create: %v", err)
	}
	wordDoc := docVal.(*vmnet.Instance)

	mainPartVal, err := wordDoc.Call("AddMainDocumentPart")
	if err != nil {
		log.Fatalf("AddMainDocumentPart: %v", err)
	}
	mainPart := mainPartVal.(*vmnet.Instance)

	text, err := openXmlAsm.New("DocumentFormat.OpenXml.Wordprocessing.Text", vmnet.String("Hello from vmnet"))
	if err != nil {
		log.Fatalf("new Text: %v", err)
	}
	run, err := openXmlAsm.New("DocumentFormat.OpenXml.Wordprocessing.Run")
	if err != nil {
		log.Fatalf("new Run: %v", err)
	}
	if _, err := run.Call("AppendChild", text); err != nil {
		log.Fatalf("Run.AppendChild(Text): %v", err)
	}
	paragraph, err := openXmlAsm.New("DocumentFormat.OpenXml.Wordprocessing.Paragraph")
	if err != nil {
		log.Fatalf("new Paragraph: %v", err)
	}
	if _, err := paragraph.Call("AppendChild", run); err != nil {
		log.Fatalf("Paragraph.AppendChild(Run): %v", err)
	}
	body, err := openXmlAsm.New("DocumentFormat.OpenXml.Wordprocessing.Body")
	if err != nil {
		log.Fatalf("new Body: %v", err)
	}
	if _, err := body.Call("AppendChild", paragraph); err != nil {
		log.Fatalf("Body.AppendChild(Paragraph): %v", err)
	}
	document, err := openXmlAsm.New("DocumentFormat.OpenXml.Wordprocessing.Document")
	if err != nil {
		log.Fatalf("new Document: %v", err)
	}
	if _, err := document.Call("AppendChild", body); err != nil {
		log.Fatalf("Document.AppendChild(Body): %v", err)
	}

	if _, err := mainPart.Call("set_Document", document); err != nil {
		log.Fatalf("MainDocumentPart.set_Document: %v", err)
	}
	if _, err := document.Call("Save"); err != nil {
		log.Fatalf("Document.Save: %v", err)
	}
	if _, err := wordDoc.Call("Dispose"); err != nil {
		log.Fatalf("WordprocessingDocument.Dispose: %v", err)
	}

	bytesVal, err := stream.Call("ToArray")
	if err != nil {
		log.Fatalf("MemoryStream.ToArray: %v", err)
	}
	data := bytesVal.Native().([]byte)

	const outPath = "report.docx"
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		log.Fatalf("writing %s: %v", outPath, err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", outPath, len(data))
}
