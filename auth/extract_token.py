#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import requests
from urllib.parse import urlparse, parse_qs

def load_config():
    """从文件加载配置"""
    config = {}
    try:
        with open('auth_config.txt', 'r', encoding='utf-8') as f:
            for line in f:
                line = line.strip()
                if '=' in line:
                    key, value = line.split('=', 1)
                    config[key] = value
        return config
    except FileNotFoundError:
        print("错误: 未找到 auth_config.txt 文件")
        print("请先运行 python generate_auth_url.py 生成授权链接")
        return None

def extract_and_exchange_token(callback_url: str):
    """从回调 URL 提取授权码并交换令牌"""
    # 加载配置
    config = load_config()
    if not config:
        return

    # 解析回调 URL
    parsed_url = urlparse(callback_url)
    query_params = parse_qs(parsed_url.query)

    code = query_params.get('code', [None])[0]
    returned_state = query_params.get('state', [None])[0]

    if not code:
        print("错误: 回调 URL 中未找到授权码")
        print("请确保复制了完整的跳转链接")
        return

    print(f"\n✓ 成功提取授权码")

    # 验证 state
    if returned_state != config['state']:
        print("警告: State 验证失败,但继续尝试交换令牌...")

    # 交换令牌
    print("\n正在交换令牌...")
    payload = {
        "grantType": "authorization_code",
        "code": code,
        "redirectUri": config['redirect_uri'],
        "clientId": config['client_id'],
        "codeVerifier": config['code_verifier']
    }

    if config['client_secret']:
        payload["clientSecret"] = config['client_secret']

    response = requests.post(
        f"https://oidc.{config['region']}.amazonaws.com/token",
        json=payload,
        headers={'Content-Type': 'application/json'}
    )

    if response.status_code != 200:
        print(f"错误: 令牌交换失败 ({response.status_code})")
        print(response.text)
        return

    tokens = response.json()
    refresh_token = tokens.get('refreshToken') or tokens.get('refresh_token')

    if not refresh_token:
        print("错误: 未获取到 refresh_token")
        return

    # 格式化输出
    credentials = f"{config['client_id']}:{config['client_secret']}:{refresh_token}"

    print("\n" + "="*80)
    print("成功获取令牌!")
    print("="*80)
    print(f"\nClient ID: {config['client_id']}")
    print(f"Client Secret: {config['client_secret'] or '(无)'}")
    print(f"Refresh Token: {refresh_token}")
    print("\n完整凭证 (格式: client_id:client_secret:refresh_token):")
    print(credentials)
    print("="*80)

    # 保存到文件
    with open('amazonq_credentials.txt', 'w', encoding='utf-8') as f:
        f.write(credentials)

    print("\n✓ 凭证已保存到 amazonq_credentials.txt")

if __name__ == "__main__":
    print("请粘贴授权后跳转的完整 URL:")
    callback_url = input().strip()

    if not callback_url:
        print("错误: URL 不能为空")
    else:
        extract_and_exchange_token(callback_url)
