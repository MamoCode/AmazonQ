#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import requests
import hashlib
import secrets
import base64
import uuid
import random
from urllib.parse import urlencode

def base64url_encode(data: bytes) -> str:
    """Base64 URL 安全编码"""
    return base64.urlsafe_b64encode(data).decode('utf-8').rstrip('=')

def generate_random_string(length: int) -> str:
    """生成随机字符串"""
    return base64url_encode(secrets.token_bytes(length))

def generate_code_challenge(verifier: str) -> str:
    """生成 code_challenge"""
    digest = hashlib.sha256(verifier.encode('utf-8')).digest()
    return base64url_encode(digest)

def generate_auth_url():
    """生成授权链接"""
    # AWS 配置
    REGION = "us-east-1"
    CLIENT_NAME = "AWS IDE Extensions for VSCode"
    START_URL = "https://view.awsapps.com/start"
    SCOPES = "codewhisperer:completions,codewhisperer:analysis,codewhisperer:conversations,codewhisperer:transformations,codewhisperer:taskassist"

    # 使用随机端口
    callback_port = random.randint(10000, 65000)
    redirect_uri = f"http://127.0.0.1:{callback_port}/oauth/callback"

    print("正在注册客户端...")
    # 注册客户端
    response = requests.post(
        f"https://oidc.{REGION}.amazonaws.com/client/register",
        json={
            "clientName": CLIENT_NAME,
            "clientType": "public",
            "grantTypes": ["authorization_code", "refresh_token"],
            "redirectUris": [redirect_uri],
            "scopes": SCOPES.split(','),
            "issuerUrl": START_URL
        },
        headers={'Content-Type': 'application/json'}
    )

    if response.status_code not in [200, 201]:
        print(f"客户端注册失败: {response.status_code}")
        print(response.text)
        return None

    registration = response.json()
    client_id = registration['clientId']
    client_secret = registration.get('clientSecret', '')

    # 生成 PKCE 参数
    code_verifier = generate_random_string(64)
    state = str(uuid.uuid4())
    code_challenge = generate_code_challenge(code_verifier)

    # 构造授权 URL
    params = {
        'response_type': 'code',
        'client_id': client_id,
        'redirect_uri': redirect_uri,
        'scopes': SCOPES,
        'state': state,
        'code_challenge': code_challenge,
        'code_challenge_method': 'S256'
    }
    auth_url = f"https://oidc.{REGION}.amazonaws.com/authorize?{urlencode(params)}"

    # 保存配置信息到文件
    config = {
        'client_id': client_id,
        'client_secret': client_secret,
        'redirect_uri': redirect_uri,
        'code_verifier': code_verifier,
        'state': state,
        'region': REGION
    }

    with open('auth_config.txt', 'w', encoding='utf-8') as f:
        for key, value in config.items():
            f.write(f"{key}={value}\n")

    print("\n" + "="*80)
    print("授权链接已生成!")
    print("="*80)
    print(f"\n授权链接:\n{auth_url}\n")
    print("="*80)
    print("\n请按以下步骤操作:")
    print("1. 复制上面的授权链接到浏览器打开")
    print("2. 使用 Google 账号登录并授权")
    print("3. 授权后会跳转到一个无法访问的页面(这是正常的)")
    print("4. 复制浏览器地址栏中完整的跳转链接")
    print("5. 运行 python extract_token.py 并粘贴跳转链接")
    print("="*80)

    return config

if __name__ == "__main__":
    generate_auth_url()
