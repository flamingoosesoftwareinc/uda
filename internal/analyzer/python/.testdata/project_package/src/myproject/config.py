from pydantic import BaseModel

class AppConfig(BaseModel):
    name: str = "default"
    debug: bool = False
