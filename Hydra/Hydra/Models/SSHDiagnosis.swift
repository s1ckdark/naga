import Foundation

struct SSHDiagnosis: Decodable, Equatable {
    let category: String
    let message: String?
    let hostname: String?
    let hostKeyFingerprint: String?

    var isHostKeyMismatch: Bool { category == "host_key_mismatch" }
    var isOK: Bool { category == "ok" }

    var humanTitle: String {
        switch category {
        case "ok": return "SSH OK"
        case "network_unreachable": return "네트워크 연결 불가"
        case "host_key_mismatch": return "호스트 키가 변경되었습니다"
        case "auth_failed": return "SSH 인증 실패"
        case "key_file_missing": return "SSH 키 파일 문제"
        case "tailscale": return "Tailscale 확인 필요"
        default: return "SSH 오류"
        }
    }
}
