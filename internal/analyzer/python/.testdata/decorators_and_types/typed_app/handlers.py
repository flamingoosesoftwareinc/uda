from typed_app.decorators import validate
from typed_app.models import Model


@validate
def handle(m: Model) -> None:
    print(f"Handling model: {m.name}")


@validate
def process(m: Model, count: int) -> str:
    return f"Processed {m.name} x{count}"
