import Foundation
import Core

func testUserCreation() {
    let user = User(id: UUID(), name: "Test")
    assert(user.name == "Test")
}
