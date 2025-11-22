"""公共工具函数"""
import os
import httpx
from typing import Dict, Optional

def get_proxies() -> Optional[Dict[str, str]]:
    """
    从环境变量获取代理配置

    Returns:
        代理字典，如果未配置则返回 None
    """
    proxy = os.getenv("HTTP_PROXY", "").strip()
    if proxy:
        return {"http": proxy, "https": proxy}
    return None

def create_proxy_mounts() -> Optional[Dict[str, httpx.AsyncHTTPTransport]]:
    """
    创建代理传输层配置

    Returns:
        代理挂载配置字典，如果没有配置代理则返回 None
    """
    proxies = get_proxies()
    if proxies:
        proxy_url = proxies.get("https") or proxies.get("http")
        if proxy_url:
            return {
                "https://": httpx.AsyncHTTPTransport(proxy=proxy_url),
                "http://": httpx.AsyncHTTPTransport(proxy=proxy_url),
            }
    return None
