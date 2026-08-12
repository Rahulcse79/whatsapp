// @wa/media-pipeline — the client media pipeline (HLD §9): per-file AES-256-GCM
// chunked encryption, SHA-256 content hashing, resumable multipart upload to
// media-svc, and the E2EE media envelope. Framework-free; real WebCrypto with
// HTTP, compression, and thumbnail generation supplied as injected ports.

export { fromBase64, toBase64 } from "./base64";
export {
  CHUNK_SIZE,
  decryptMedia,
  encryptMedia,
  encryptWithKey,
  sha256,
  type EncryptedMedia,
} from "./mediaCrypto";
export type { MediaEnvelope } from "./envelope";
export {
  partCount,
  ResumableUploader,
  type CreateUploadResult,
  type PartETag,
  type PartURL,
  type UploadOutcome,
  type UploadTransport,
} from "./uploader";
export {
  MediaPipeline,
  type Compressor,
  type DownloadTransport,
  type MediaSource,
  type Preview,
  type ThumbnailMaker,
} from "./pipeline";
