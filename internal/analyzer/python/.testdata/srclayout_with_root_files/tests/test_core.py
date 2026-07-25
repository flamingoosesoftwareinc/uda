from hypothesis_jsonschema import from_schema

def test_basic():
    result = from_schema({"type": "object"})
    assert isinstance(result, dict)
