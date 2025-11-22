"""Amazon Q API 客户端"""
import json
import uuid
import asyncio
from typing import Dict, Optional, Tuple, List, AsyncGenerator, Any
import httpx

from ..utils.helpers import get_proxies, create_proxy_mounts
from ..config.settings import AMAZONQ_API_URL, DEFAULT_HEADERS
from .parser import EventStreamParser, extract_event_info

class StreamTracker:
    """流响应跟踪器"""

    def __init__(self):
        self.has_content = False

    async def track(self, gen: AsyncGenerator[str, None]) -> AsyncGenerator[str, None]:
        """
        跟踪生成器是否产生内容

        Args:
            gen: 异步生成器

        Yields:
            生成器的内容
        """
        async for item in gen:
            if item:
                self.has_content = True
            yield item

def load_template() -> Tuple[str, Dict[str, str]]:
    """
    加载 Amazon Q API 请求模板

    Returns:
        (url, headers): API 端点 URL 和默认请求头
    """
    return AMAZONQ_API_URL, DEFAULT_HEADERS.copy()

def _merge_headers(as_log: Dict[str, str], bearer_token: str) -> Dict[str, str]:
    """
    合并并更新请求头

    Args:
        as_log: 基础请求头
        bearer_token: 认证令牌

    Returns:
        合并后的请求头
    """
    headers = dict(as_log)
    for k in list(headers.keys()):
        kl = k.lower()
        if kl in ("content-length", "host", "connection", "transfer-encoding"):
            headers.pop(k, None)

    def set_header(name: str, value: str):
        for key in list(headers.keys()):
            if key.lower() == name.lower():
                del headers[key]
        headers[name] = value

    set_header("Authorization", f"Bearer {bearer_token}")
    set_header("amz-sdk-invocation-id", str(uuid.uuid4()))
    return headers

async def send_chat_request(
    access_token: str,
    messages: List[Dict[str, Any]],
    model: Optional[str] = None,
    stream: bool = False,
    timeout: Tuple[int, int] = (30, 300),
    client: Optional[httpx.AsyncClient] = None,
    raw_payload: Dict[str, Any] = None
) -> Tuple[Optional[str], Optional[AsyncGenerator[str, None]], StreamTracker, Optional[AsyncGenerator[Any, None]]]:
    """
    发送聊天请求到 Amazon Q API

    Args:
        access_token: Amazon Q access token
        messages: 消息列表（已废弃，使用 raw_payload）
        model: 模型名称（已废弃，使用 raw_payload）
        stream: 是否流式响应
        timeout: 超时配置 (连接超时, 读取超时)
        client: HTTP 客户端
        raw_payload: Claude API 转换后的请求体（必需）

    Returns:
        (text, stream_gen, tracker, event_iter): 响应文本、流生成器、跟踪器、事件迭代器
    """
    if raw_payload is None:
        raise ValueError("raw_payload is required")

    url, headers_from_log = load_template()
    headers_from_log["amz-sdk-invocation-id"] = str(uuid.uuid4())

    # 使用原始 payload（用于 Claude API）
    body_json = raw_payload
    # 确保 conversationId 已设置
    if "conversationState" in body_json and "conversationId" not in body_json["conversationState"]:
        body_json["conversationState"]["conversationId"] = str(uuid.uuid4())

    payload_str = json.dumps(body_json, ensure_ascii=False)
    headers = _merge_headers(headers_from_log, access_token)

    local_client = False
    if client is None:
        local_client = True
        mounts = create_proxy_mounts()
        # 增加连接超时时间，避免 TLS 握手超时
        timeout_config = httpx.Timeout(connect=60.0, read=timeout[1], write=timeout[0], pool=10.0)
        # 只在有代理时才传递 mounts 参数
        if mounts:
            client = httpx.AsyncClient(mounts=mounts, timeout=timeout_config)
        else:
            client = httpx.AsyncClient(timeout=timeout_config)

    # 使用手动请求发送以控制流生命周期
    req = client.build_request("POST", url, headers=headers, content=payload_str)

    resp = None
    try:
        resp = await client.send(req, stream=True)

        if resp.status_code >= 400:
            try:
                await resp.read()
                err = resp.text
            except Exception:
                err = f"HTTP {resp.status_code}"
            await resp.aclose()
            if local_client:
                await client.aclose()
            raise httpx.HTTPError(f"Upstream error {resp.status_code}: {err}")

        tracker = StreamTracker()

        # 跟踪响应是否已被消费，避免重复关闭
        response_consumed = False

        async def _iter_events() -> AsyncGenerator[Any, None]:
            nonlocal response_consumed
            try:
                # 使用 parser.py 中的 EventStreamParser
                async def byte_gen():
                    async for chunk in resp.aiter_bytes():
                        if chunk:
                            yield chunk

                async for message in EventStreamParser.parse_stream(byte_gen()):
                    event_info = extract_event_info(message)
                    if event_info:
                        event_type = event_info.get('event_type')
                        payload = event_info.get('payload')
                        if event_type and payload:
                            yield (event_type, payload)
            except Exception:
                if not tracker.has_content:
                    raise
            finally:
                response_consumed = True
                await resp.aclose()
                if local_client:
                    await client.aclose()

        if stream:
            # 包装生成器以确保早期终止时清理资源
            async def _safe_iter_events():
                try:
                    # 托底方案: 300秒强制超时
                    async with asyncio.timeout(300):
                        async for item in _iter_events():
                            yield item
                except asyncio.TimeoutError:
                    # 超时强制关闭
                    if resp and not resp.is_closed:
                        await resp.aclose()
                    if local_client and client:
                        await client.aclose()
                    raise
                except GeneratorExit:
                    # 生成器在未完全消费时被关闭
                    # 即使 finally 块未执行也要确保清理
                    if resp and not resp.is_closed:
                        await resp.aclose()
                    if local_client and client:
                        await client.aclose()
                    raise
                except Exception:
                    # 任何异常都应触发清理
                    if resp and not resp.is_closed:
                        await resp.aclose()
                    if local_client and client:
                        await client.aclose()
                    raise
            return None, None, tracker, _safe_iter_events()
        else:
            # 非流式：消费所有事件
            try:
                async for _ in _iter_events():
                    pass
            finally:
                # 即使迭代未完成也要确保响应已关闭
                if not response_consumed and resp:
                    await resp.aclose()
                    if local_client:
                        await client.aclose()
            return None, None, tracker, None

    except Exception:
        # 关键：在创建生成器之前的任何异常都要关闭响应
        if resp and not resp.is_closed:
            await resp.aclose()
        if local_client and client:
            await client.aclose()
        raise
