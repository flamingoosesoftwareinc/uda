def cache(func):
    """Simple caching decorator."""
    memo = {}

    def wrapper(*args):
        if args not in memo:
            memo[args] = func(*args)
        return memo[args]

    return wrapper


def validate(func):
    """Validation decorator."""

    def wrapper(*args, **kwargs):
        for arg in args:
            if arg is None:
                raise ValueError("None not allowed")
        return func(*args, **kwargs)

    return wrapper
