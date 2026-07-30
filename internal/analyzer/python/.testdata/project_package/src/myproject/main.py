from myproject.config import AppConfig
from myproject.handlers import APIHandler

def main():
    config = AppConfig(name="myapp", debug=True)
    handler = APIHandler(config)
    result = handler.handle_request("token123", {"key": "value"})
    print(result)

if __name__ == "__main__":
    main()
