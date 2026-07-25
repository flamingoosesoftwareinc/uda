import json


class User:
    def __init__(self, name: str):
        self.name = name

    def to_json(self) -> str:
        return json.dumps({"name": self.name})


class Profile:
    def __init__(self, user: "User", bio: str = ""):
        self.user = user
        self.bio = bio
