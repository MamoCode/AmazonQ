"""FastAPI 主服务"""
import json
import os
import traceback
import uuid
import asyncio
from pathlib import Path
from typing import Dict, Optional, Any
from contextlib import asynccontextmanager

from fastapi import FastAPI, Depends, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import StreamingResponse, RedirectResponse
from dotenv import load_dotenv
import httpx

from ..core.types import ClaudeRequest
from ..core.converter import convert_claude_to_amazonq_request
from ..amazonq.client import send_chat_request
from ..amazonq.streaming import ClaudeStreamHandler
from ..utils.helpers import create_proxy_mounts
from .middleware import auth_middleware, refresh_all_tokens

# 基础目录
BASE_DIR = Path(__file__).resolve().parent.parent.parent

# 加载环境变量
load_dotenv(BASE_DIR / ".env")

# 全局 HTTP 客户端
GLOBAL_CLIENT: Optional[httpx.AsyncClient] = None

async def _init_global_client():
    """初始化全局 HTTP 客户端"""
    global GLOBAL_CLIENT
    mounts = create_proxy_mounts()

    limits = httpx.Limits(
        max_keepalive_connections=60,
        max_connections=60,
        keepalive_expiry=30.0
    )

    timeout = httpx.Timeout(
        connect=30.0,
        read=300.0,
        write=30.0,
        pool=10.0
    )

    if mounts:
        GLOBAL_CLIENT = httpx.AsyncClient(mounts=mounts, timeout=timeout, limits=limits)
    else:
        GLOBAL_CLIENT = httpx.AsyncClient(timeout=timeout, limits=limits)

async def _close_global_client():
    """关闭全局 HTTP 客户端"""
    global GLOBAL_CLIENT
    if GLOBAL_CLIENT:
        await GLOBAL_CLIENT.aclose()
        GLOBAL_CLIENT = None

async def _global_token_refresher():
    """全局 Token 刷新任务：每 45 分钟刷新所有缓存的 token"""
    while True:
        try:
            await asyncio.sleep(45 * 60)
            await refresh_all_tokens()
        except Exception:
            traceback.print_exc()
            await asyncio.sleep(60)

@asynccontextmanager
async def lifespan(app_instance: FastAPI):
    """
    管理应用生命周期事件
    启动时初始化客户端和后台任务，关闭时清理资源
    """
    await _init_global_client()
    asyncio.create_task(_global_token_refresher())
    yield
    await _close_global_client()

# 创建 FastAPI 应用
app = FastAPI(
    title="Amazon Q Proxy - OpenAI-compatible Server",
    lifespan=lifespan
)

# CORS 中间件
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

@app.get("/")
async def root():
    """根路径重定向"""
    return RedirectResponse(url="https://www.bilibili.com/video/BV1SMH5zfEwe/?spm_id_from=333.337.search-card.all.click&vd_source=1f3b8eb28230105c578a443fa6481550")

@app.post("/v1/messages")
async def claude_messages(
    req: ClaudeRequest,
    account: Dict[str, Any] = Depends(auth_middleware)
):
    """
    Claude-compatible messages endpoint

    Args:
        req: Claude API 请求
        account: 认证信息

    Returns:
        Claude API 响应
    """
    # 1. 转换请求
    try:
        aq_request = convert_claude_to_amazonq_request(req)
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=400, detail=f"Request conversion failed: {str(e)}")

    # 2. 发送上游请求 - 始终使用流式获取完整事件详情
    try:
        access = account.get("accessToken")
        if not access:
            raise HTTPException(status_code=502, detail="Access token unavailable")

        # 调用流式接口获取事件迭代器
        _, _, tracker, event_iter = await send_chat_request(
            access_token=access,
            messages=[],
            model=req.model,
            stream=True,
            client=GLOBAL_CLIENT,
            raw_payload=aq_request
        )

        if not event_iter:
            raise HTTPException(status_code=502, detail="No event stream returned")

        # 流处理器
        input_tokens = 0
        handler = ClaudeStreamHandler(model=req.model, input_tokens=input_tokens)

        async def event_generator():
            try:
                async for event_type, payload in event_iter:
                    async for sse in handler.handle_event(event_type, payload):
                        yield sse
                async for sse in handler.finish():
                    yield sse
            except GeneratorExit:
                raise
            except Exception:
                raise

        if req.stream:
            return StreamingResponse(event_generator(), media_type="text/event-stream")
        else:
            # 非流式：累积响应
            content_blocks = []
            usage = {"input_tokens": 0, "output_tokens": 0}
            stop_reason = None

            final_content = []

            async for sse_line in event_generator():
                if sse_line.startswith("data: "):
                    data_str = sse_line[6:].strip()
                    if data_str == "[DONE]":
                        continue
                    try:
                        data = json.loads(data_str)
                        dtype = data.get("type")
                        if dtype == "content_block_start":
                            idx = data.get("index", 0)
                            while len(final_content) <= idx:
                                final_content.append(None)
                            final_content[idx] = data.get("content_block")
                        elif dtype == "content_block_delta":
                            idx = data.get("index", 0)
                            delta = data.get("delta", {})
                            if final_content[idx]:
                                if delta.get("type") == "text_delta":
                                    final_content[idx]["text"] += delta.get("text", "")
                                elif delta.get("type") == "input_json_delta":
                                    if "partial_json" not in final_content[idx]:
                                        final_content[idx]["partial_json"] = ""
                                    final_content[idx]["partial_json"] += delta.get("partial_json", "")
                        elif dtype == "content_block_stop":
                            idx = data.get("index", 0)
                            if final_content[idx] and final_content[idx]["type"] == "tool_use":
                                if "partial_json" in final_content[idx]:
                                    try:
                                        final_content[idx]["input"] = json.loads(final_content[idx]["partial_json"])
                                    except:
                                        pass
                                    del final_content[idx]["partial_json"]
                        elif dtype == "message_delta":
                            usage = data.get("usage", usage)
                            stop_reason = data.get("delta", {}).get("stop_reason")
                    except:
                        pass

            return {
                "id": f"msg_{uuid.uuid4()}",
                "type": "message",
                "role": "assistant",
                "model": req.model,
                "content": [c for c in final_content if c is not None],
                "stop_reason": stop_reason,
                "stop_sequence": None,
                "usage": usage
            }

    except Exception as e:
        raise

if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8000"))
    uvicorn.run(
        app,
        host="0.0.0.0",
        port=port,
        log_level="info"
    )
