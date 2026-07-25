from plugins.db import connection


def log(message: str) -> None:
    print(message)


def get_db():
    return connection.get_connection()
