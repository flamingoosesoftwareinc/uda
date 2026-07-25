from mypackage.config import Config
import requests

def handle(cfg: Config) -> str:
    response = requests.get("https://example.com")
    return f"Handled {cfg.name}: {response.status_code}"
