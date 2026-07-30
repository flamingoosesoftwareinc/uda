from ..config import AppConfig

class AuthHandler:
    def __init__(self, config: AppConfig):
        self.config = config

    def authenticate(self, token: str) -> bool:
        return len(token) > 0
