import XCTest
@testable import Hydra

final class SSHDiagnosisTests: XCTestCase {

    private func decode(_ json: String) throws -> SSHDiagnosis {
        let data = Data(json.utf8)
        return try JSONDecoder().decode(SSHDiagnosis.self, from: data)
    }

    func testDecodeMinimalOK() throws {
        let d = try decode(#"{"category":"ok"}"#)
        XCTAssertTrue(d.isOK)
        XCTAssertFalse(d.isHostKeyMismatch)
        XCTAssertNil(d.hostname)
        XCTAssertNil(d.hostKeyFingerprint)
        XCTAssertEqual(d.humanTitle, "SSH OK")
    }

    func testDecodeHostKeyMismatchWithFingerprint() throws {
        let json = """
        {"category":"host_key_mismatch","hostname":"100.1.2.3","hostKeyFingerprint":"SHA256:AAAA"}
        """
        let d = try decode(json)
        XCTAssertTrue(d.isHostKeyMismatch)
        XCTAssertFalse(d.isOK)
        XCTAssertEqual(d.hostname, "100.1.2.3")
        XCTAssertEqual(d.hostKeyFingerprint, "SHA256:AAAA")
        XCTAssertEqual(d.humanTitle, "호스트 키가 변경되었습니다")
    }

    func testHumanTitleCoversAllCategories() throws {
        let mapping: [(String, String)] = [
            ("ok", "SSH OK"),
            ("network_unreachable", "네트워크 연결 불가"),
            ("host_key_mismatch", "호스트 키가 변경되었습니다"),
            ("auth_failed", "SSH 인증 실패"),
            ("key_file_missing", "SSH 키 파일 문제"),
            ("tailscale", "Tailscale 확인 필요"),
            ("future_unknown_category", "SSH 오류"),
        ]
        for (category, expectedTitle) in mapping {
            let d = try decode(#"{"category":"\#(category)"}"#)
            XCTAssertEqual(d.humanTitle, expectedTitle, "category=\(category)")
        }
    }

    func testEquality() throws {
        let a = try decode(#"{"category":"ok","hostname":"h"}"#)
        let b = try decode(#"{"category":"ok","hostname":"h"}"#)
        let c = try decode(#"{"category":"ok","hostname":"different"}"#)
        XCTAssertEqual(a, b)
        XCTAssertNotEqual(a, c)
    }
}
