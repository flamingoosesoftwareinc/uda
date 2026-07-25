from plugins.db import connection
from plugins.utils import helpers


def authenticate(username: str, password: str) -> bool:
    conn = connection.get_connection()
    helpers.log("authenticating user")
    return True
