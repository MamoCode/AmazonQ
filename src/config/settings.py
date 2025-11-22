"""Amazon Q API 配置"""

# Amazon Q API 端点
AMAZONQ_API_URL = "https://q.us-east-1.amazonaws.com/"

# 默认请求头模板
DEFAULT_HEADERS = {
    "content-type": "application/x-amz-json-1.0",
    "x-amz-target": "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
    "user-agent": "aws-sdk-rust/1.3.9 ua/2.1 api/codewhispererstreaming/0.1.11582 os/windows lang/rust/1.87.0 md/appVersion-1.19.4 app/AmazonQ-For-CLI",
    "x-amz-user-agent": "aws-sdk-rust/1.3.9 ua/2.1 api/codewhispererstreaming/0.1.11582 os/windows lang/rust/1.87.0 m/F app/AmazonQ-For-CLI",
    "x-amzn-codewhisperer-optout": "false",
    "amz-sdk-request": "attempt=1; max=3"
}

# 默认请求体模板（仅作为结构参考，实际使用时会被 raw_payload 替换）
DEFAULT_BODY_TEMPLATE = {
    "conversationState": {
        "conversationId": "",
        "history": [],
        "currentMessage": {
            "userInputMessage": {
                "content": "",
                "userInputMessageContext": {
                    "envState": {
                        "operatingSystem": "windows",
                        "currentWorkingDirectory": ""
                    },
                    "tools": []
                },
                "origin": "CLI",
                "modelId": "claude-sonnet-4"
            }
        },
        "chatTriggerType": "MANUAL"
    }
}
