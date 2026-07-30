from flask import Flask, jsonify
from core.models import User
from core.service import UserService

def create_app() -> Flask:
    app = Flask(__name__)
    service = UserService()

    @app.route("/users/<int:user_id>")
    def get_user(user_id: int):
        user = service.get_user(user_id)
        if user:
            return jsonify({"id": user.id, "name": user.name})
        return jsonify({"error": "not found"}), 404

    return app
