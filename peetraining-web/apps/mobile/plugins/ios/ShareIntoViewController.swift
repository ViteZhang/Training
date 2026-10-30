import UIKit
import Social
import UniformTypeIdentifiers

// 分享扩展只复制到 App Group 并提示用户返回 App；不使用 UIApplication/Responder 链打开主 App。
@MainActor
class ShareIntoViewController: SLComposeServiceViewController {
  private var saving = false
  override func viewDidLoad() {
    super.viewDidLoad()
    title = "导入学习资料"
    placeholder = "文件会保存到待导入列表。保存后打开 App，选择专业课即可继续。"
    navigationItem.rightBarButtonItem?.title = "保存待导入"
  }
  override func isContentValid() -> Bool { !saving }
  override func didSelectPost() {
    guard !saving else { return }
    saving = true
    validateContent()
    Task {
      do {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "AppGroupId") as? String,
              let container = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group),
              let defaults = UserDefaults(suiteName: group) else { throw ReceiveError.unavailable }
        var pending: [SharePayload] = []
        if let data = defaults.data(forKey: SHARE_INTO_DEFAULTS_KEY) {
          pending = (try? JSONDecoder().decode([SharePayload].self, from: data)) ?? []
        }
        let items = extensionContext?.inputItems as? [NSExtensionItem] ?? []
        let providers = items.flatMap { $0.attachments ?? [] }
        guard !providers.isEmpty, providers.count <= 30 else { throw ReceiveError.unsupported }
        var incoming: [SharePayload] = []
        var created: [URL] = []
        do {
          for provider in providers {
            guard let identifier = provider.registeredTypeIdentifiers.first(where: {
              guard let type = UTType($0) else { return false }
              return type.conforms(to: .pdf) || type.conforms(to: .image) ||
                type.preferredFilenameExtension == "docx" || type.preferredFilenameExtension == "xlsx" || type.conforms(to: .fileURL)
            }) else { throw ReceiveError.unsupported }
            let (url, name, size) = try await copyFile(provider, identifier: identifier, container: container)
            created.append(url)
            let ext = url.pathExtension.lowercased()
            guard ["pdf", "docx", "xlsx", "jpg", "jpeg", "png", "heic", "heif", "webp"].contains(ext), size > 0, size <= 50 * 1024 * 1024 else { throw ReceiveError.unsupported }
            let type = UTType(filenameExtension: ext)
            incoming.append(SharePayload(type: type?.conforms(to: .image) == true ? .image : .file,
              value: url.absoluteString, mimeType: type?.preferredMIMEType ?? "application/octet-stream",
              metadata: ShareMetadata(originalName: name, size: size)))
          }
          // 同一批次全部落盘后才发布清单，失败时不影响先前分享的文件。
          pending.append(contentsOf: incoming)
          guard pending.count <= 40 else { throw ReceiveError.full }
          defaults.set(try JSONEncoder().encode(pending), forKey: SHARE_INTO_DEFAULTS_KEY)
        } catch {
          for url in created { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
          throw error
        }
        extensionContext?.completeRequest(returningItems: nil)
      } catch {
        saving = false
        validateContent()
        let alert = UIAlertController(title: "暂时无法保存", message: "请确认文件已下载，且为 PDF、docx、xlsx 或图片，每个不超过 50MB。也可以先保存到手机，再打开 App 导入。待导入过多时，请先处理原有文件。", preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "知道了", style: .default))
        present(alert, animated: true)
      }
    }
  }
  private func copyFile(_ provider: NSItemProvider, identifier: String, container: URL) async throws -> (URL, String, Int) {
    let suggestedName = provider.suggestedName
    return try await withCheckedThrowingContinuation { continuation in
      provider.loadFileRepresentation(forTypeIdentifier: identifier) { source, error in
        do {
          if let error { throw error }
          guard let source else { throw ReceiveError.unavailable }
          let size = (try FileManager.default.attributesOfItem(atPath: source.path)[.size] as? NSNumber)?.intValue ?? 0
          guard size > 0, size <= 50 * 1024 * 1024 else { throw ReceiveError.unsupported }
          let directory = container.appendingPathComponent("shared-import-" + UUID().uuidString, isDirectory: true)
          try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
          var name = (suggestedName ?? source.lastPathComponent).replacingOccurrences(of: "/", with: "_")
          if (name as NSString).pathExtension.isEmpty {
            let ext = source.pathExtension.isEmpty ? (UTType(identifier)?.preferredFilenameExtension ?? "") : source.pathExtension
            if !ext.isEmpty { name += "." + ext }
          }
          let target = directory.appendingPathComponent(name)
          try FileManager.default.copyItem(at: source, to: target)
          continuation.resume(returning: (target, name, size))
        } catch { continuation.resume(throwing: error) }
      }
    }
  }
  private enum ReceiveError: Error { case unavailable, unsupported, full }
}
