"""Amazon Q Event Stream 解析器"""
import json
import struct
import logging
from typing import Optional, Dict, Any, AsyncIterator

logger = logging.getLogger(__name__)

class EventStreamParser:
    """AWS Event Stream 二进制格式解析器"""

    @staticmethod
    def parse_headers(headers_data: bytes) -> Dict[str, str]:
        """
        解析事件流头部

        Args:
            headers_data: 头部二进制数据

        Returns:
            解析后的头部字典
        """
        headers = {}
        offset = 0

        while offset < len(headers_data):
            if offset >= len(headers_data):
                break
            name_length = headers_data[offset]
            offset += 1

            if offset + name_length > len(headers_data):
                break
            name = headers_data[offset:offset + name_length].decode('utf-8')
            offset += name_length

            if offset >= len(headers_data):
                break
            value_type = headers_data[offset]
            offset += 1

            if offset + 2 > len(headers_data):
                break
            value_length = struct.unpack('>H', headers_data[offset:offset + 2])[0]
            offset += 2

            if offset + value_length > len(headers_data):
                break

            if value_type == 7:
                value = headers_data[offset:offset + value_length].decode('utf-8')
            else:
                value = headers_data[offset:offset + value_length]

            offset += value_length
            headers[name] = value

        return headers

    @staticmethod
    def parse_message(data: bytes) -> Optional[Dict[str, Any]]:
        """
        解析单个 Event Stream 消息

        Args:
            data: 消息二进制数据

        Returns:
            解析后的消息字典
        """
        try:
            if len(data) < 16:
                return None

            total_length = struct.unpack('>I', data[0:4])[0]
            headers_length = struct.unpack('>I', data[4:8])[0]

            if len(data) < total_length:
                logger.warning(f"Incomplete message: expected {total_length} bytes, got {len(data)}")
                return None

            headers_data = data[12:12 + headers_length]
            headers = EventStreamParser.parse_headers(headers_data)

            payload_start = 12 + headers_length
            payload_end = total_length - 4
            payload_data = data[payload_start:payload_end]

            payload = None
            if payload_data:
                try:
                    payload = json.loads(payload_data.decode('utf-8'))
                except (json.JSONDecodeError, UnicodeDecodeError):
                    payload = payload_data

            return {
                'headers': headers,
                'payload': payload,
                'total_length': total_length
            }

        except Exception as e:
            logger.error(f"Failed to parse message: {e}", exc_info=True)
            return None

    @staticmethod
    async def parse_stream(byte_stream: AsyncIterator[bytes]) -> AsyncIterator[Dict[str, Any]]:
        """
        解析字节流并提取事件

        Args:
            byte_stream: 异步字节流

        Yields:
            解析后的事件消息
        """
        buffer = bytearray()

        async for chunk in byte_stream:
            buffer.extend(chunk)

            while len(buffer) >= 12:
                try:
                    total_length = struct.unpack('>I', buffer[0:4])[0]
                except struct.error:
                    break

                if len(buffer) < total_length:
                    break

                message_data = bytes(buffer[:total_length])
                buffer = buffer[total_length:]

                message = EventStreamParser.parse_message(message_data)
                if message:
                    yield message

def extract_event_info(message: Dict[str, Any]) -> Optional[Dict[str, Any]]:
    """
    从解析后的消息中提取事件信息

    Args:
        message: 解析后的消息

    Returns:
        事件信息字典
    """
    headers = message.get('headers', {})
    payload = message.get('payload')

    event_type = headers.get(':event-type') or headers.get('event-type')
    content_type = headers.get(':content-type') or headers.get('content-type')
    message_type = headers.get(':message-type') or headers.get('message-type')

    return {
        'event_type': event_type,
        'content_type': content_type,
        'message_type': message_type,
        'payload': payload
    }

def _sse_format(event_type: str, data: Dict[str, Any]) -> str:
    """
    格式化 SSE 事件

    Args:
        event_type: 事件类型
        data: 事件数据

    Returns:
        SSE 格式字符串
    """
    json_data = json.dumps(data, ensure_ascii=False)
    return f"event: {event_type}\ndata: {json_data}\n\n"

def build_message_start(conversation_id: str, model: str = "claude-sonnet-4.5", input_tokens: int = 0) -> str:
    """构建 message_start SSE 事件"""
    data = {
        "type": "message_start",
        "message": {
            "id": conversation_id,
            "type": "message",
            "role": "assistant",
            "content": [],
            "model": model,
            "stop_reason": None,
            "stop_sequence": None,
            "usage": {"input_tokens": input_tokens, "output_tokens": 0}
        }
    }
    return _sse_format("message_start", data)

def build_content_block_start(index: int, block_type: str = "text") -> str:
    """构建 content_block_start SSE 事件"""
    data = {
        "type": "content_block_start",
        "index": index,
        "content_block": {"type": block_type, "text": ""} if block_type == "text" else {"type": block_type}
    }
    return _sse_format("content_block_start", data)

def build_content_block_delta(index: int, text: str) -> str:
    """构建 content_block_delta SSE 事件（文本）"""
    data = {
        "type": "content_block_delta",
        "index": index,
        "delta": {"type": "text_delta", "text": text}
    }
    return _sse_format("content_block_delta", data)

def build_content_block_stop(index: int) -> str:
    """构建 content_block_stop SSE 事件"""
    data = {
        "type": "content_block_stop",
        "index": index
    }
    return _sse_format("content_block_stop", data)

def build_ping() -> str:
    """构建 ping SSE 事件"""
    data = {"type": "ping"}
    return _sse_format("ping", data)

def build_message_stop(input_tokens: int, output_tokens: int, stop_reason: Optional[str] = None) -> str:
    """构建 message_delta 和 message_stop SSE 事件"""
    delta_data = {
        "type": "message_delta",
        "delta": {"stop_reason": stop_reason or "end_turn", "stop_sequence": None},
        "usage": {"output_tokens": output_tokens}
    }
    delta_event = _sse_format("message_delta", delta_data)

    stop_data = {
        "type": "message_stop"
    }
    stop_event = _sse_format("message_stop", stop_data)

    return delta_event + stop_event

def build_tool_use_start(index: int, tool_use_id: str, tool_name: str) -> str:
    """构建 tool_use content_block_start SSE 事件"""
    data = {
        "type": "content_block_start",
        "index": index,
        "content_block": {
            "type": "tool_use",
            "id": tool_use_id,
            "name": tool_name,
            "input": {}
        }
    }
    return _sse_format("content_block_start", data)

def build_tool_use_input_delta(index: int, input_json_delta: str) -> str:
    """构建 tool_use input_json_delta SSE 事件"""
    data = {
        "type": "content_block_delta",
        "index": index,
        "delta": {
            "type": "input_json_delta",
            "partial_json": input_json_delta
        }
    }
    return _sse_format("content_block_delta", data)
