import json

class Config:
    def __init__(self, name: str = "default"):
        self.name = name

    def to_json(self) -> str:
        return json.dumps({"name": self.name})
