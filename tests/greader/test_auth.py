import os

import google_reader
import pytest
import requests

YARR_URL = os.environ.get("YARR_URL", "http://localhost:7070")
USERNAME = os.environ.get("YARR_USERNAME", "test")
PASSWORD = os.environ.get("YARR_PASSWORD", "test")


def test_login_valid_credentials(client):
    auth = client.login(USERNAME, PASSWORD)
    assert auth.AccessToken
    assert auth.TokenType == "GoogleLogin"


def test_login_invalid_credentials(client):
    with pytest.raises(google_reader.AuthenticationError):
        client.login(USERNAME, "wrongpassword")


def test_login_invalid_username(client):
    with pytest.raises(google_reader.AuthenticationError):
        client.login("wronguser", PASSWORD)


def test_token_endpoint(client, auth):
    token = client.get_token(auth)
    assert token
    assert len(token) > 0


def test_unauthorized_without_header():
    resp = requests.get(f"{YARR_URL}/reader/api/0/tag/list")
    assert resp.status_code == 401
