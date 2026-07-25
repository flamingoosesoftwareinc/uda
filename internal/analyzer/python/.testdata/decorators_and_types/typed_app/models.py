from typed_app.decorators import cache


class Model:
    def __init__(self, name: str, value: int):
        self.name = name
        self.value = value

    @cache
    def compute(self, x: int) -> int:
        return self.value * x
