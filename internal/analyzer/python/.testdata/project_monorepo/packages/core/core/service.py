from core.models import User

class UserService:
    def __init__(self):
        self.users: list[User] = []

    def add_user(self, user: User) -> None:
        self.users.append(user)

    def get_user(self, user_id: int) -> User | None:
        for user in self.users:
            if user.id == user_id:
                return user
        return None
