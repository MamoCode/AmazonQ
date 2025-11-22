"""Claude API 到 Amazon Q API 的请求转换器"""
import json
import uuid
from datetime import datetime
from typing import List, Dict, Any, Optional, Union

from .types import ClaudeRequest, ClaudeMessage, ClaudeTool

def get_current_timestamp() -> str:
    """获取当前时间戳（Amazon Q 格式）"""
    now = datetime.now().astimezone()
    weekday = now.strftime("%A")
    iso_time = now.isoformat(timespec='milliseconds')
    return f"{weekday}, {iso_time}"

def map_model_name(claude_model: str) -> str:
    """
    将 Claude 模型名称映射到 Amazon Q 模型 ID

    Args:
        claude_model: Claude 模型名称

    Returns:
        Amazon Q 模型 ID
    """
    model_lower = claude_model.lower()
    if model_lower.startswith("claude-sonnet-4.5") or model_lower.startswith("claude-sonnet-4-5"):
        return "claude-sonnet-4.5"
    return "claude-sonnet-4"

def extract_text_from_content(content: Union[str, List[Dict[str, Any]]]) -> str:
    """
    从 Claude 内容中提取文本

    Args:
        content: Claude 消息内容

    Returns:
        提取的文本内容
    """
    if isinstance(content, str):
        return content
    elif isinstance(content, list):
        parts = []
        for block in content:
            if isinstance(block, dict):
                if block.get("type") == "text":
                    parts.append(block.get("text", ""))
        return "\n".join(parts)
    return ""

def process_tool_result_block(block: Dict[str, Any], tool_results: List[Dict[str, Any]]) -> None:
    """
    处理单个 tool_result 块，提取内容并添加到 tool_results 列表

    Args:
        block: tool_result 类型的内容块
        tool_results: 用于存储处理结果的列表
    """
    tool_use_id = block.get("tool_use_id")
    raw_c = block.get("content", [])

    aq_content = []
    if isinstance(raw_c, str):
        aq_content = [{"text": raw_c}]
    elif isinstance(raw_c, list):
        for item in raw_c:
            if isinstance(item, dict):
                if item.get("type") == "text":
                    aq_content.append({"text": item.get("text", "")})
                elif "text" in item:
                    aq_content.append({"text": item["text"]})
            elif isinstance(item, str):
                aq_content.append({"text": item})

    if not any(i.get("text", "").strip() for i in aq_content):
        aq_content = [{"text": "Tool use was cancelled by the user"}]

    # 合并已存在的工具结果
    existing = next((r for r in tool_results if r["toolUseId"] == tool_use_id), None)
    if existing:
        existing["content"].extend(aq_content)
    else:
        tool_results.append({
            "toolUseId": tool_use_id,
            "content": aq_content,
            "status": block.get("status", "success")
        })

def extract_images_from_content(content: Union[str, List[Dict[str, Any]]]) -> Optional[List[Dict[str, Any]]]:
    """
    从 Claude 内容中提取图片并转换为 Amazon Q 格式

    Args:
        content: Claude 消息内容

    Returns:
        Amazon Q 格式的图片列表
    """
    if not isinstance(content, list):
        return None

    images = []
    for block in content:
        if isinstance(block, dict) and block.get("type") == "image":
            source = block.get("source", {})
            if source.get("type") == "base64":
                media_type = source.get("media_type", "image/png")
                fmt = media_type.split("/")[-1] if "/" in media_type else "png"
                images.append({
                    "format": fmt,
                    "source": {
                        "bytes": source.get("data", "")
                    }
                })
    return images if images else None

def convert_tool(tool: ClaudeTool) -> Dict[str, Any]:
    """
    将 Claude 工具转换为 Amazon Q 工具格式

    Args:
        tool: Claude 工具定义

    Returns:
        Amazon Q 工具格式
    """
    desc = tool.description or ""
    if len(desc) > 10240:
        desc = desc[:10100] + "\n\n...(Full description provided in TOOL DOCUMENTATION section)"

    return {
        "toolSpecification": {
            "name": tool.name,
            "description": desc,
            "inputSchema": {"json": tool.input_schema}
        }
    }

def merge_user_messages(messages: List[Dict[str, Any]]) -> Dict[str, Any]:
    """
    合并连续的用户消息，仅保留最后 2 条消息的图片

    Args:
        messages: 用户消息列表

    Returns:
        合并后的消息
    """
    if not messages:
        return {}

    all_contents = []
    base_context = None
    base_origin = None
    base_model = None
    all_images = []

    for msg in messages:
        content = msg.get("content", "")
        if base_context is None:
            base_context = msg.get("userInputMessageContext", {})
        if base_origin is None:
            base_origin = msg.get("origin", "CLI")
        if base_model is None:
            base_model = msg.get("modelId")

        if content:
            all_contents.append(content)

        # 收集每条消息的图片
        msg_images = msg.get("images")
        if msg_images:
            all_images.append(msg_images)

    result = {
        "content": "\n\n".join(all_contents),
        "userInputMessageContext": base_context or {},
        "origin": base_origin or "CLI",
        "modelId": base_model
    }

    # 仅保留最后 2 条消息的图片
    if all_images:
        kept_images = []
        for img_list in all_images[-2:]:
            kept_images.extend(img_list)
        if kept_images:
            result["images"] = kept_images

    return result

def process_history(messages: List[ClaudeMessage]) -> List[Dict[str, Any]]:
    """
    处理历史消息，转换为 Amazon Q 格式（交替的 user/assistant）

    Args:
        messages: Claude 消息列表

    Returns:
        Amazon Q 格式的历史消息
    """
    history = []
    seen_tool_use_ids = set()

    raw_history = []

    # 第一遍：转换单个消息
    for msg in messages:
        if msg.role == "user":
            content = msg.content
            text_content = ""
            tool_results = None
            images = extract_images_from_content(content)

            if isinstance(content, list):
                text_parts = []
                for block in content:
                    if isinstance(block, dict):
                        btype = block.get("type")
                        if btype == "text":
                            text_parts.append(block.get("text", ""))
                        elif btype == "tool_result":
                            if tool_results is None:
                                tool_results = []
                            process_tool_result_block(block, tool_results)
                text_content = "\n".join(text_parts)
            else:
                text_content = extract_text_from_content(content)

            user_ctx = {
                "envState": {
                    "operatingSystem": "macos",
                    "currentWorkingDirectory": "/"
                }
            }
            if tool_results:
                user_ctx["toolResults"] = tool_results

            u_msg = {
                "content": text_content,
                "userInputMessageContext": user_ctx,
                "origin": "CLI"
            }
            if images:
                u_msg["images"] = images

            raw_history.append({"userInputMessage": u_msg})

        elif msg.role == "assistant":
            content = msg.content
            text_content = extract_text_from_content(content)

            entry = {
                "assistantResponseMessage": {
                    "messageId": str(uuid.uuid4()),
                    "content": text_content
                }
            }

            if isinstance(content, list):
                tool_uses = []
                for block in content:
                    if isinstance(block, dict) and block.get("type") == "tool_use":
                        tid = block.get("id")
                        if tid and tid not in seen_tool_use_ids:
                            seen_tool_use_ids.add(tid)
                            tool_uses.append({
                                "toolUseId": tid,
                                "name": block.get("name"),
                                "input": block.get("input", {})
                            })
                if tool_uses:
                    entry["assistantResponseMessage"]["toolUses"] = tool_uses

            raw_history.append(entry)

    # 第二遍：合并连续的用户消息
    pending_user_msgs = []
    for item in raw_history:
        if "userInputMessage" in item:
            pending_user_msgs.append(item["userInputMessage"])
        elif "assistantResponseMessage" in item:
            if pending_user_msgs:
                merged = merge_user_messages(pending_user_msgs)
                history.append({"userInputMessage": merged})
                pending_user_msgs = []
            history.append(item)

    if pending_user_msgs:
        merged = merge_user_messages(pending_user_msgs)
        history.append({"userInputMessage": merged})

    return history

def convert_claude_to_amazonq_request(req: ClaudeRequest, conversation_id: Optional[str] = None) -> Dict[str, Any]:
    """
    将 Claude 请求转换为 Amazon Q 请求体

    Args:
        req: Claude API 请求对象
        conversation_id: 会话 ID（可选）

    Returns:
        Amazon Q API 请求体
    """
    if conversation_id is None:
        conversation_id = str(uuid.uuid4())

    # 1. 工具转换
    aq_tools = []
    long_desc_tools = []
    if req.tools:
        for t in req.tools:
            if t.description and len(t.description) > 10240:
                long_desc_tools.append({"name": t.name, "full_description": t.description})
            aq_tools.append(convert_tool(t))

    # 2. 当前消息（最后一条用户消息）
    last_msg = req.messages[-1] if req.messages else None
    prompt_content = ""
    tool_results = None
    has_tool_result = False
    images = None

    if last_msg and last_msg.role == "user":
        content = last_msg.content
        images = extract_images_from_content(content)

        if isinstance(content, list):
            text_parts = []
            for block in content:
                if isinstance(block, dict):
                    btype = block.get("type")
                    if btype == "text":
                        text_parts.append(block.get("text", ""))
                    elif btype == "tool_result":
                        has_tool_result = True
                        if tool_results is None:
                            tool_results = []
                        process_tool_result_block(block, tool_results)
            prompt_content = "\n".join(text_parts)
        else:
            prompt_content = extract_text_from_content(content)

    # 3. 上下文构建
    user_ctx = {
        "envState": {
            "operatingSystem": "macos",
            "currentWorkingDirectory": "/"
        }
    }
    if aq_tools:
        user_ctx["tools"] = aq_tools
    if tool_results:
        user_ctx["toolResults"] = tool_results

    # 4. 格式化内容
    formatted_content = ""
    if has_tool_result and not prompt_content:
        formatted_content = ""
    else:
        formatted_content = (
            "--- CONTEXT ENTRY BEGIN ---\n"
            f"Current time: {get_current_timestamp()}\n"
            "--- CONTEXT ENTRY END ---\n\n"
            "--- USER MESSAGE BEGIN ---\n"
            f"{prompt_content}\n"
            "--- USER MESSAGE END ---"
        )

    if long_desc_tools:
        docs = []
        for info in long_desc_tools:
            docs.append(f"Tool: {info['name']}\nFull Description:\n{info['full_description']}\n")
        formatted_content = (
            "--- TOOL DOCUMENTATION BEGIN ---\n"
            f"{''.join(docs)}"
            "--- TOOL DOCUMENTATION END ---\n\n"
            f"{formatted_content}"
        )

    if req.system and formatted_content:
        sys_text = ""
        if isinstance(req.system, str):
            sys_text = req.system
        elif isinstance(req.system, list):
            parts = []
            for b in req.system:
                if isinstance(b, dict) and b.get("type") == "text":
                    parts.append(b.get("text", ""))
            sys_text = "\n".join(parts)

        if sys_text:
            formatted_content = (
                "--- SYSTEM PROMPT BEGIN ---\n"
                f"{sys_text}\n"
                "--- SYSTEM PROMPT END ---\n\n"
                f"{formatted_content}"
            )

    # 5. 模型映射
    model_id = map_model_name(req.model)

    # 6. 用户输入消息
    user_input_msg = {
        "content": formatted_content,
        "userInputMessageContext": user_ctx,
        "origin": "CLI",
        "modelId": model_id
    }
    if images:
        user_input_msg["images"] = images

    # 7. 历史消息处理
    history_msgs = req.messages[:-1] if len(req.messages) > 1 else []
    aq_history = process_history(history_msgs)

    # 8. 最终请求体
    return {
        "conversationState": {
            "conversationId": conversation_id,
            "history": aq_history,
            "currentMessage": {
                "userInputMessage": user_input_msg
            },
            "chatTriggerType": "MANUAL"
        }
    }
