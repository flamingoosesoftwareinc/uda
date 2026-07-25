from ..config import AppConfig
from .auth import AuthHandler

class APIHandler:
    def __init__(self, config: AppConfig):
        self.config = config
        self.auth = AuthHandler(config)

    def handle_request(self, token: str, data: dict) -> dict:
        if not self.auth.authenticate(token):
            return {"error": "unauthorized"}
        return {"status": "ok", "data": data}
