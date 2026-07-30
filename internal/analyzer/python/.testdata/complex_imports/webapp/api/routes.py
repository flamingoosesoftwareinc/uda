from ..models import User
from ..services.auth import verify as check_auth


def get_users():
    check_auth()
    return [User("alice"), User("bob")]
