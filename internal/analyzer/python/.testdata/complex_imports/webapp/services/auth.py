from webapp.models import User
import hashlib


def verify():
    return True


def hash_password(password: str) -> str:
    return hashlib.sha256(password.encode()).hexdigest()
