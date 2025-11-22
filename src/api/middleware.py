"""API 认证中间件"""
import uuid
import time
import hashlib
import traceback
from typing import Dict, Optional, Any, Tuple
from fastapi import Header, HTTPException
import httpx

# Token 缓存: {hash: {accessToken, refreshToken, clientId, clientSecret, lastRefresh}}
TOKEN_MAP: Dict[str, Dict[str, Any]] = {}

# OIDC 配置
OIDC_BASE = "https://oidc.us-east-1.amazonaws.com"
TOKEN_URL = f"{OIDC_BASE}/token"

def _sha256(text: str) -> str:
    """
    计算 SHA256 哈希

    Args:
        text: 输入文本

    Returns:
        SHA256 哈希值
    """
    return hashlib.sha256(text.encode()).hexdigest()

def _parse_bearer_token(bearer_token: str) -> Tuple[str, str, str]:
    """
    解析 Bearer token: clientId:clientSecret:refreshToken
    注意: refreshToken 中可能包含冒号，需要正确处理

    Args:
        bearer_token: Bearer token 字符串

    Returns:
        (client_id, client_secret, refresh_token)
    """
    temp_array = bearer_token.split(":")
    client_id = temp_array[0] if len(temp_array) > 0 else ""
    client_secret = temp_array[1] if len(temp_array) > 1 else ""
    refresh_token = ":".join(temp_array[2:]) if len(temp_array) > 2 else ""
    return client_id, client_secret, refresh_token

def _oidc_headers() -> Dict[str, str]:
    """
    生成 OIDC 请求头

    Returns:
        OIDC 请求头字典
    """
    return {
        "content-type": "application/json",
        "user-agent": "aws-sdk-rust/1.3.9 os/windows lang/rust/1.87.0",
        "x-amz-user-agent": "aws-sdk-rust/1.3.9 ua/2.1 api/ssooidc/1.88.0 os/windows lang/rust/1.87.0 m/E app/AmazonQ-For-CLI",
        "amz-sdk-request": "attempt=1; max=3",
        "amz-sdk-invocation-id": str(uuid.uuid4()),
    }

async def _handle_token_refresh(
    client_id: str,
    client_secret: str,
    refresh_token: str,
    client: Optional[httpx.AsyncClient] = None
) -> Optional[str]:
    """
    刷新 access token

    Args:
        client_id: 客户端 ID
        client_secret: 客户端密钥
        refresh_token: 刷新令牌
        client: HTTP 客户端

    Returns:
        新的 access token
    """
    payload = {
        "grantType": "refresh_token",
        "clientId": client_id,
        "clientSecret": client_secret,
        "refreshToken": refresh_token,
    }

    try:
        if not client:
            async with httpx.AsyncClient(timeout=60.0) as temp_client:
                r = await temp_client.post(TOKEN_URL, headers=_oidc_headers(), json=payload)
                r.raise_for_status()
                data = r.json()
        else:
            r = await client.post(TOKEN_URL, headers=_oidc_headers(), json=payload)
            r.raise_for_status()
            data = r.json()

        return data.get("accessToken")
    except httpx.HTTPStatusError as e:
        print(f"Token refresh HTTP error: {e.response.status_code} - {e.response.text}")
        traceback.print_exc()
        return None
    except Exception as e:
        print(f"Token refresh error: {e}")
        traceback.print_exc()
        return None

async def auth_middleware(
    authorization: Optional[str] = Header(default=None),
    x_api_key: Optional[str] = Header(default=None, alias="x-api-key")
) -> Dict[str, Any]:
    """
    认证中间件：支持 OpenAI Bearer token 和 Claude x-api-key
    Token 格式: clientId:clientSecret:refreshToken

    Args:
        authorization: Authorization 头
        x_api_key: x-api-key 头

    Returns:
        认证信息字典

    Raises:
        HTTPException: 认证失败
    """
    # 优先使用 x-api-key（Claude 格式）
    token = x_api_key if x_api_key else None

    # 如果没有 x-api-key，尝试从 Authorization header 获取（OpenAI 格式）
    if not token and authorization and authorization.startswith("Bearer "):
        token = authorization[7:]

    if not token:
        raise HTTPException(
            status_code=401,
            detail="Missing authentication. Provide Authorization header or x-api-key"
        )

    token_hash = _sha256(token)

    # 检查缓存
    if token_hash in TOKEN_MAP:
        return {
            "accessToken": TOKEN_MAP[token_hash]["accessToken"],
            "clientId": TOKEN_MAP[token_hash]["clientId"],
            "clientSecret": TOKEN_MAP[token_hash]["clientSecret"],
            "refreshToken": TOKEN_MAP[token_hash]["refreshToken"],
        }

    # 解析 token
    client_id, client_secret, refresh_token = _parse_bearer_token(token)

    if not client_id or not client_secret or not refresh_token:
        raise HTTPException(
            status_code=401,
            detail="Invalid token format. Expected: clientId:clientSecret:refreshToken"
        )

    # 刷新 token
    access_token = await _handle_token_refresh(client_id, client_secret, refresh_token)
    if not access_token:
        raise HTTPException(status_code=401, detail="Failed to refresh access token")

    # 缓存
    TOKEN_MAP[token_hash] = {
        "accessToken": access_token,
        "refreshToken": refresh_token,
        "clientId": client_id,
        "clientSecret": client_secret,
        "lastRefresh": time.time()
    }

    return {
        "accessToken": access_token,
        "clientId": client_id,
        "clientSecret": client_secret,
        "refreshToken": refresh_token,
    }

async def refresh_all_tokens():
    """全局刷新器：刷新所有缓存的 token"""
    if not TOKEN_MAP:
        return

    print(f"[Token Refresher] Starting token refresh cycle...")
    refresh_count = 0

    for hash_key, token_data in list(TOKEN_MAP.items()):
        try:
            new_token = await _handle_token_refresh(
                token_data["clientId"],
                token_data["clientSecret"],
                token_data["refreshToken"]
            )
            if new_token:
                TOKEN_MAP[hash_key]["accessToken"] = new_token
                TOKEN_MAP[hash_key]["lastRefresh"] = time.time()
                refresh_count += 1
            else:
                print(f"[Token Refresher] Failed to refresh token for hash: {hash_key[:8]}...")
        except Exception as e:
            print(f"[Token Refresher] Exception refreshing token: {e}")
            traceback.print_exc()

    print(f"[Token Refresher] Refreshed {refresh_count}/{len(TOKEN_MAP)} tokens")
