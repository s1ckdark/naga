import Foundation

/// Stores task results locally when offline, syncs when reconnected
actor OfflineQueue {
    static let shared = OfflineQueue()

    private let fileURL: URL
    private var queue: [QueuedResult] = []

    struct QueuedResult: Codable {
        let taskId: String
        let result: [String: String] // simplified for Codable
        let queuedAt: Date
    }

    private init() {
        let docs = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first!
        fileURL = docs.appendingPathComponent("offline_queue.json")
        load()
    }

    func enqueue(taskId: String, result: [String: String]) {
        let item = QueuedResult(taskId: taskId, result: result, queuedAt: Date())
        queue.append(item)
        save()
    }

    func dequeueAll() -> [QueuedResult] {
        let items = queue
        queue.removeAll()
        save()
        return items
    }

    var count: Int { queue.count }
    var isEmpty: Bool { queue.isEmpty }

    private func save() {
        if let data = try? JSONEncoder().encode(queue) {
            try? data.write(to: fileURL)
        }
    }

    private func load() {
        guard let data = try? Data(contentsOf: fileURL),
              let items = try? JSONDecoder().decode([QueuedResult].self, from: data) else { return }
        queue = items
    }
}
