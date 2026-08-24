import { useEffect, useState } from "react";
import { api, type KnowledgeDocument, type KnowledgeDocumentChunk } from "../api/client";

type OpsDocumentsAPI = Pick<typeof api, "listKnowledgeDocuments" | "getKnowledgeDocument" | "retryKnowledgeDocument" | "uploadKnowledgeDocument">;

export default function OpsDocumentsPage({ api: client = api }: { api?: OpsDocumentsAPI }) {
  const [documents, setDocuments] = useState<KnowledgeDocument[]>([]);
  const [selected, setSelected] = useState<KnowledgeDocument | null>(null);
  const [chunks, setChunks] = useState<KnowledgeDocumentChunk[]>([]);
  const [status, setStatus] = useState("ALL");
  const [message, setMessage] = useState("");
  const [uploadProductID, setUploadProductID] = useState("1001");
  const [uploadDocType, setUploadDocType] = useState("FAQ");
  const [uploadFile, setUploadFile] = useState<File | null>(null);

  useEffect(() => {
    void load();
  }, [status]);

  async function load() {
    const result = await client.listKnowledgeDocuments(status === "ALL" ? {} : { status });
    setDocuments(result.documents);
  }

  async function open(documentNo: string) {
    const result = await client.getKnowledgeDocument(documentNo);
    setSelected(result.document);
    setChunks(result.chunks);
  }

  async function retry() {
    if (!selected) return;
    const result = await client.retryKnowledgeDocument(selected.document_no);
    setSelected(result.document);
    setDocuments((items) => items.map((item) => (item.document_no === result.document.document_no ? result.document : item)));
    setMessage("Retry submitted");
  }

  async function upload(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!uploadFile) return;
    const contentBase64 = await fileToBase64(uploadFile);
    const result = await client.uploadKnowledgeDocument({
      product_id: Number(uploadProductID),
      doc_type: uploadDocType,
      file_name: uploadFile.name,
      content_type: uploadFile.type || "application/octet-stream",
      content_base64: contentBase64,
    });
    setDocuments((items) => [result.document, ...items.filter((item) => item.document_no !== result.document.document_no)]);
    setSelected(result.document);
    setChunks([]);
    setMessage("Upload submitted");
  }

  return (
    <section className="pageStack">
      <section className="statusBanner compactBanner">
        <div>
          <p className="eyebrow">Operations</p>
          <h1>商品资料</h1>
          <p>上传、版本状态、失败原因和重试入口。</p>
        </div>
      </section>
      <div className="opsLayout">
        <aside className="sidePanel">
          <form className="opsUploadForm" onSubmit={(event) => void upload(event)}>
            <label>
              Product
              <input aria-label="Upload product" inputMode="numeric" value={uploadProductID} onChange={(event) => setUploadProductID(event.target.value)} />
            </label>
            <label>
              Type
              <select aria-label="Upload document type" value={uploadDocType} onChange={(event) => setUploadDocType(event.target.value)}>
                <option>DETAIL</option>
                <option>SPEC</option>
                <option>FAQ</option>
                <option>AFTER_SALE</option>
              </select>
            </label>
            <label>
              File
              <input aria-label="Upload file" type="file" onChange={(event) => setUploadFile(event.target.files?.[0] ?? null)} />
            </label>
            <button className="secondaryButton" disabled={!uploadFile || !Number(uploadProductID)}>Upload</button>
          </form>
          <div className="panelHeader">
            <h2>Documents</h2>
            <select aria-label="Document status" value={status} onChange={(event) => setStatus(event.target.value)}>
              <option>ALL</option>
              <option>PENDING</option>
              <option>PROCESSING</option>
              <option>READY</option>
              <option>FAILED</option>
            </select>
          </div>
          <div className="timeline">
            {documents.map((document) => (
              <button className="opsListButton" key={document.document_no} aria-label={document.document_no} onClick={() => void open(document.document_no)}>
                <strong>{document.document_no}</strong>
                <span>{document.doc_type} v{document.version}</span>
                <em>{document.status}</em>
              </button>
            ))}
          </div>
        </aside>
        <section className="detailMain">
          {selected ? (
            <>
              <div className="panelHeader">
                <h2>{selected.document_no}</h2>
                <span className="stockBadge">{selected.status}</span>
              </div>
              <dl className="snapshotList">
                <div><dt>Product</dt><dd>{selected.product_id}</dd></div>
                <div><dt>Error</dt><dd>{selected.error_code || "-"}</dd></div>
                <div><dt>Chunks</dt><dd>{selected.chunk_count ?? chunks.length}</dd></div>
              </dl>
              {selected.error_message ? <p className="errorPanel">{selected.error_message}</p> : null}
              <div className="actionRow">
                <button className="primaryButton" disabled={selected.status !== "FAILED"} onClick={() => void retry()}>Retry</button>
                {message ? <span className="successText">{message}</span> : null}
              </div>
              <div className="sourceList">
                {chunks.map((chunk) => (
                  <div className="sourceItem" key={chunk.chunk_id}>
                    <strong>#{chunk.chunk_index} {chunk.status}</strong>
                    <span>{chunk.section || "-"} page {chunk.source_page || "-"}</span>
                    <p>{chunk.content_hash}</p>
                  </div>
                ))}
              </div>
            </>
          ) : (
            <div className="statePanel">选择一个资料版本查看详情。</div>
          )}
        </section>
      </div>
    </section>
  );
}

function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("file_read_failed"));
    reader.onload = () => {
      const result = String(reader.result ?? "");
      resolve(result.includes(",") ? result.slice(result.indexOf(",") + 1) : result);
    };
    reader.readAsDataURL(file);
  });
}
