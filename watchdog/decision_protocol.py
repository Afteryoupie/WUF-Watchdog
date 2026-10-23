"""Shared Laya wire contract, independent of model/process management."""

MODEL_ID = "laya-multilingual"


def validate_payload(payload):
    if not isinstance(payload, dict) or payload.get("state") is None:
        raise ValueError("Expected a JSON object with state and questions")
    if payload.get("model", MODEL_ID) not in (MODEL_ID, "jev-latest", "default", ""):
        raise ValueError("Unknown decision model")
    questions = payload.get("questions")
    if not isinstance(questions, dict) or not 1 <= len(questions) <= 32:
        raise ValueError("Expected 1–32 questions")
    for key, question in questions.items():
        if not isinstance(key, str) or len(key) > 128 or not isinstance(question, dict):
            raise ValueError("Invalid question")
        if question.get("type") not in ("noul", "choice", "score"):
            raise ValueError("Question type must be noul, choice or score")
        if not isinstance(question.get("instructions"), str) or len(question["instructions"]) > 4000:
            raise ValueError("Expected question instructions up to 4000 characters")
    return payload
