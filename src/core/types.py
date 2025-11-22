"""Claude API 请求/响应类型定义"""
from typing import List, Optional, Union, Dict, Any
from pydantic import BaseModel

class ClaudeMessage(BaseModel):
    role: str
    content: Union[str, List[Dict[str, Any]]]

class ClaudeTool(BaseModel):
    name: str
    description: Optional[str] = ""
    input_schema: Dict[str, Any]

class ClaudeRequest(BaseModel):
    model: str
    messages: List[ClaudeMessage]
    max_tokens: int = 8192
    temperature: Optional[float] = None
    tools: Optional[List[ClaudeTool]] = None
    stream: bool = False
    system: Optional[Union[str, List[Dict[str, Any]]]] = None
