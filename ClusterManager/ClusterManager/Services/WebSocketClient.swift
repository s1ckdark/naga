import Foundation

/// WebSocket message types matching server protocol
enum WSMessageType: String, Codable {
    case taskAssign = "task.assign"
    case taskResult = "task.result"
    case taskCancel = "task.cancel"
    case taskStatus = "task.status"
    case capabilityRegister = "capability.register"
    case metrics = "metrics"
    case ping = "ping"
    case pong = "pong"
}

/// WebSocket message envelope
struct WSMessage: Codable {
    let type: WSMessageType
    var deviceId: String?
    var taskId: String?
    var payload: Data?
    var timestamp: Date

    init(type: WSMessageType, deviceId: String? = nil, taskId: String? = nil, payload: Data? = nil) {
        self.type = type
        self.deviceId = deviceId
        self.taskId = taskId
        self.payload = payload
        self.timestamp = Date()
    }
}

/// Manages WebSocket connection to cluster server
@MainActor
class WebSocketClient: ObservableObject {
    @Published var isConnected = false
    @Published var lastError: String?

    private var webSocketTask: URLSessionWebSocketTask?
    private var session: URLSession
    private let deviceId: String
    private var serverURL: URL?
    private var reconnectTask: Task<Void, Never>?

    var onTaskReceived: ((ServerTask) -> Void)?
    var onTaskCancelled: ((String) -> Void)?

    init(deviceId: String) {
        self.deviceId = deviceId
        self.session = URLSession(configuration: .default)
    }

    func connect(to serverURL: URL) {
        self.serverURL = serverURL
        var components = URLComponents(url: serverURL, resolvingAgainstBaseURL: false)!
        components.scheme = serverURL.scheme == "https" ? "wss" : "ws"
        components.path = "/ws"
        components.queryItems = [URLQueryItem(name: "device_id", value: deviceId)]

        guard let wsURL = components.url else { return }

        webSocketTask = session.webSocketTask(with: wsURL)
        webSocketTask?.resume()
        isConnected = true
        lastError = nil

        receiveMessage()
    }

    func disconnect() {
        reconnectTask?.cancel()
        webSocketTask?.cancel(with: .normalClosure, reason: nil)
        webSocketTask = nil
        isConnected = false
    }

    func send(_ message: WSMessage) async throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let data = try encoder.encode(message)
        try await webSocketTask?.send(.string(String(data: data, encoding: .utf8)!))
    }

    func sendTaskResult(taskId: String, result: [String: Any]) async {
        do {
            let resultData = try JSONSerialization.data(withJSONObject: result)
            let message = WSMessage(
                type: .taskResult,
                deviceId: deviceId,
                taskId: taskId,
                payload: resultData
            )
            try await send(message)
        } catch {
            lastError = error.localizedDescription
        }
    }

    private func receiveMessage() {
        webSocketTask?.receive { [weak self] result in
            Task { @MainActor in
                guard let self = self else { return }

                switch result {
                case .success(let message):
                    switch message {
                    case .string(let text):
                        self.handleMessage(text)
                    case .data(let data):
                        if let text = String(data: data, encoding: .utf8) {
                            self.handleMessage(text)
                        }
                    @unknown default:
                        break
                    }
                    self.receiveMessage() // Continue listening

                case .failure(let error):
                    self.isConnected = false
                    self.lastError = error.localizedDescription
                    self.scheduleReconnect()
                }
            }
        }
    }

    private func handleMessage(_ text: String) {
        guard let data = text.data(using: .utf8) else { return }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601

        guard let msg = try? decoder.decode(WSMessage.self, from: data) else { return }

        switch msg.type {
        case .taskAssign:
            if let payload = msg.payload,
               let task = try? decoder.decode(ServerTask.self, from: payload) {
                onTaskReceived?(task)
            }
        case .taskCancel:
            if let taskId = msg.taskId {
                onTaskCancelled?(taskId)
            }
        case .ping:
            Task {
                try? await self.send(WSMessage(type: .pong, deviceId: self.deviceId))
            }
        default:
            break
        }
    }

    private func scheduleReconnect() {
        reconnectTask?.cancel()
        reconnectTask = Task {
            try? await Task.sleep(for: .seconds(5))
            if !Task.isCancelled, let url = serverURL {
                connect(to: url)
            }
        }
    }
}

/// Task received from server
struct ServerTask: Codable, Identifiable {
    let id: String
    let type: String
    let priority: String
    let requiredCapabilities: [String]
    let payload: [String: AnyCodable]?
    let timeout: Int?

    struct AnyCodable: Codable {
        let value: Any

        init(_ value: Any) { self.value = value }

        init(from decoder: Decoder) throws {
            let container = try decoder.singleValueContainer()
            if let str = try? container.decode(String.self) { value = str }
            else if let int = try? container.decode(Int.self) { value = int }
            else if let double = try? container.decode(Double.self) { value = double }
            else if let bool = try? container.decode(Bool.self) { value = bool }
            else { value = "" }
        }

        func encode(to encoder: Encoder) throws {
            var container = encoder.singleValueContainer()
            if let str = value as? String { try container.encode(str) }
            else if let int = value as? Int { try container.encode(int) }
            else if let double = value as? Double { try container.encode(double) }
            else if let bool = value as? Bool { try container.encode(bool) }
        }
    }
}
