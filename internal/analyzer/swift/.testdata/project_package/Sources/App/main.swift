import Foundation
import Core

let service = UserService()
let user = service.fetchUser()
print("User: \(user.name)")
