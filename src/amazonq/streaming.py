"""Amazon Q 流式响应处理器"""
import json
import logging
from typing import AsyncGenerator, Optional, Dict, Any, List, Set

from .parser import (
    build_message_start,
    build_content_block_start,
    build_content_block_delta,
    build_content_block_stop,
    build_ping,
    build_message_stop,
    build_tool_use_start,
    build_tool_use_input_delta
)

logger = logging.getLogger(__name__)

class ClaudeStreamHandler:
    """Claude SSE 流处理器，将 Amazon Q 事件转换为 Claude API 格式"""

    def __init__(self, model: str, input_tokens: int = 0):
        """
        初始化流处理器

        Args:
            model: 模型名称
            input_tokens: 输入 token 数量
        """
        self.model = model
        self.input_tokens = input_tokens
        self.response_buffer: List[str] = []
        self.content_block_index: int = -1
        self.content_block_started: bool = False
        self.content_block_start_sent: bool = False
        self.content_block_stop_sent: bool = False
        self.message_start_sent: bool = False
        self.conversation_id: Optional[str] = None

        # 工具使用状态
        self.current_tool_use: Optional[Dict[str, Any]] = None
        self.tool_input_buffer: List[str] = []
        self.tool_use_id: Optional[str] = None
        self.tool_name: Optional[str] = None
        self._processed_tool_use_ids: Set[str] = set()
        self.all_tool_inputs: List[str] = []

    async def handle_event(self, event_type: str, payload: Dict[str, Any]) -> AsyncGenerator[str, None]:
        """
        处理单个 Amazon Q 事件并生成 Claude SSE 事件

        Args:
            event_type: 事件类型
            payload: 事件数据

        Yields:
            Claude SSE 格式的事件字符串
        """

        # 1. 消息开始 (initial-response)
        if event_type == "initial-response":
            if not self.message_start_sent:
                conv_id = payload.get('conversationId', self.conversation_id or 'unknown')
                self.conversation_id = conv_id
                yield build_message_start(conv_id, self.model, self.input_tokens)
                self.message_start_sent = True
                yield build_ping()

        # 2. 内容块增量 (assistantResponseEvent)
        elif event_type == "assistantResponseEvent":
            content = payload.get("content", "")

            # 关闭任何打开的工具使用块
            if self.current_tool_use and not self.content_block_stop_sent:
                yield build_content_block_stop(self.content_block_index)
                self.content_block_stop_sent = True
                self.current_tool_use = None

            # 如果需要，启动内容块
            if not self.content_block_start_sent:
                self.content_block_index += 1
                yield build_content_block_start(self.content_block_index, "text")
                self.content_block_start_sent = True
                self.content_block_started = True

            # 发送增量
            if content:
                self.response_buffer.append(content)
                yield build_content_block_delta(self.content_block_index, content)

        # 3. 工具使用 (toolUseEvent)
        elif event_type == "toolUseEvent":
            tool_use_id = payload.get("toolUseId")
            tool_name = payload.get("name")
            tool_input = payload.get("input", {})
            is_stop = payload.get("stop", False)

            # 启动新的工具使用
            if tool_use_id and tool_name and not self.current_tool_use:
                # 关闭之前的文本块
                if self.content_block_start_sent and not self.content_block_stop_sent:
                    yield build_content_block_stop(self.content_block_index)
                    self.content_block_stop_sent = True

                self._processed_tool_use_ids.add(tool_use_id)
                self.content_block_index += 1

                yield build_tool_use_start(self.content_block_index, tool_use_id, tool_name)

                self.content_block_started = True
                self.current_tool_use = {"toolUseId": tool_use_id, "name": tool_name}
                self.tool_use_id = tool_use_id
                self.tool_name = tool_name
                self.tool_input_buffer = []
                self.content_block_stop_sent = False
                self.content_block_start_sent = True

            # 累积输入
            if self.current_tool_use and tool_input:
                fragment = ""
                if isinstance(tool_input, str):
                    fragment = tool_input
                else:
                    fragment = json.dumps(tool_input, ensure_ascii=False)

                self.tool_input_buffer.append(fragment)
                yield build_tool_use_input_delta(self.content_block_index, fragment)

            # 停止工具使用
            if is_stop and self.current_tool_use:
                full_input = "".join(self.tool_input_buffer)
                self.all_tool_inputs.append(full_input)

                yield build_content_block_stop(self.content_block_index)
                self.content_block_stop_sent = True
                self.content_block_started = False
                self.current_tool_use = None
                self.tool_use_id = None
                self.tool_name = None
                self.tool_input_buffer = []

        # 4. 助手响应结束 (assistantResponseEnd)
        elif event_type == "assistantResponseEnd":
            # 关闭任何打开的块
            if self.content_block_started and not self.content_block_stop_sent:
                yield build_content_block_stop(self.content_block_index)
                self.content_block_stop_sent = True

    async def finish(self) -> AsyncGenerator[str, None]:
        """
        发送最终事件

        Yields:
            最终的 SSE 事件
        """
        # 确保最后一个块已关闭
        if self.content_block_started and not self.content_block_stop_sent:
            yield build_content_block_stop(self.content_block_index)
            self.content_block_stop_sent = True

        # 计算输出 token（近似）
        full_text = "".join(self.response_buffer)
        full_tool_input = "".join(self.all_tool_inputs)
        # 简单近似：4 个字符为 1 个 token
        output_tokens = max(1, (len(full_text) + len(full_tool_input)) // 4)

        yield build_message_stop(self.input_tokens, output_tokens, "end_turn")
