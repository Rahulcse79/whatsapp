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
  decryptThumbnail,
  fetchAndDecrypt,
  type DownloadTransport,
} from "./download";
export {
  MediaPipeline,
  type Compressor,
  type MediaSource,
  type Preview,
  type ThumbnailMaker,
} from "./pipeline";
export {
  DownloadManager,
  type DownloadItem,
  type DownloadManagerOptions,
  type DownloadState,
} from "./downloadManager";
export {
  classifyMedia,
  downloadName,
  formatBytes,
  formatDuration,
  guessExtension,
  isVoiceNote,
  type MediaKind,
} from "./mediaMeta";
export {
  blurhashAverageColor,
  blurhashCssColor,
  decodeBlurhash,
} from "./blurhash";
export {
  encodeMediaMessage,
  encodeTextMessage,
  encodeReaction,
  parseReaction,
  type QuotedRef,
  type ReactionBody,
  parseMediaMessage,
  parseTextMessage,
  type MediaMessageBody,
  type TextMessageBody,
} from "./messageBody";
export {
  detectFirstUrl,
  generateLinkPreview,
  isHttpUrl,
  parseLinkMetadata,
  type GeneratePreviewOptions,
  type HtmlFetcher,
  type ImageEmbedder,
  type LinkMetadata,
  type LinkPreview,
  type PreviewFetch,
} from "./linkPreview";
export {
  decryptBackup,
  encryptBackup,
  newBackupSalt,
  type BackupKeyDeriver,
} from "./backup";
