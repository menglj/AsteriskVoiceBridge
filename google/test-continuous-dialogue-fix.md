# 连续对话功能修复

## 问题分析

从最新日志分析，发现了两个关键问题：

### 1. 进步：可以处理2句 ✅
- 第一句："What's your name?" → 翻译 → TTS播放
- 第二句："How you?" → 中断检测 → 取消TTS → 翻译 → TTS播放

### 2. 问题1：TTS文本堆栈后wakefile未发送 ❌
- **第二句被中断后**：第三句被堆栈（`TTS:AddText Event=TextStacked`）
- **TTS播放完成后**：没有发送`wakefile`信号
- **结果**：`deadfile`状态未重置，音频流中断

### 3. 问题2：中断后STT未恢复 ❌
- **中断检测后**：STT一直处于中断模式
- **心跳状态**：`STT:heartbeat Event="SilentPacketSent - InterruptMode"`
- **结果**：无法接收新的用户语音

## 解决方案

### 修复1：TTS播放完成后总是发送wakefile

#### 问题原因
```go
// 原来的逻辑：只在textStack为空时发送wakefile
if text != "" {
    // 播放堆栈文本，但不发送wakefile
} else {
    // 发送wakefile
}
```

#### 修复方案
```go
// 修复后：无论textStack状态如何，都发送wakefile
if text != "" {
    // 播放堆栈文本
} else {
    // 文本堆栈为空
}
// 总是发送wakefile信号
go func() {
    time.Sleep(100 * time.Millisecond)
    tts.wakefile <- true
    log.Info("TTS:listenForPlaybackDone", "Event", "WakeFileSent")
}()
```

### 修复2：中断后恢复STT

#### 问题原因
```go
// 原来的逻辑：中断后没有恢复STT
if level == "interrupt" {
    // 取消TTS播放
    // 处理中断文本
    // 但没有恢复STT
}
```

#### 修复方案
```go
// 修复后：中断后立即恢复STT
if level == "interrupt" {
    // 取消TTS播放
    // 恢复STT
    if useGoogle {
        if v.googleSTTProvider != nil {
            v.googleSTTProvider.ResumeCall(callid)
        }
    }
    // 处理中断文本
}
```

## 修复后的工作流程

### 正常对话流程
```
用户说话 → STT识别 → 翻译 → 暂停STT → TTS播放 → 发送wakefile → 恢复STT → 准备下一句
```

### 中断对话流程
```
用户说话 → STT识别 → 翻译 → 暂停STT → TTS播放
    ↓
用户打断 → 中断检测 → 取消TTS → 恢复STT → 处理中断文本 → 暂停STT → TTS播放 → 发送wakefile → 恢复STT
```

### 文本堆栈流程
```
第一句TTS播放 → 第二句被堆栈 → 第一句完成 → 发送wakefile → 播放第二句 → 发送wakefile → 恢复STT
```

## 预期结果

### 连续对话测试
1. **第一句**：用户说"Hello"
2. **第二句**：用户说"How are you?"
3. **第三句**：用户说"What's your name?"
4. **第四句**：用户说"Goodbye"
5. **验证**：所有句子都应该正常识别、翻译和播放

### 中断对话测试
1. **开始TTS播放**：系统播放翻译结果
2. **用户打断**：在TTS播放期间说话
3. **验证**：应该检测到打断，取消TTS，处理用户语音
4. **继续对话**：用户说下一句话
5. **验证**：应该正常识别和翻译

### 日志输出示例
```
# 第一句
STT:MessageResponse Text="Hello"
BOT:HandleTranslationResults translated=你好
STT:Pause callid=xxx status=PausedWithInterrupt
TTS:AddText Event=TextSent
STT:heartbeat Event=SilentPacketSent - InterruptMode
TTS:listenForPlaybackDone Event=WakeFileSent
RTP:StreamFrom Event=WakeFile
STT:Resume callid=xxx status=Resumed
STT:heartbeat Event=SilentPacketSent - NormalMode

# 第二句
STT:MessageResponse Text="How are you?"
BOT:HandleTranslationResults translated=你好吗？
STT:Pause callid=xxx status=PausedWithInterrupt
TTS:AddText Event=TextSent
STT:heartbeat Event=SilentPacketSent - InterruptMode

# 用户打断
STT:InterruptDetection callid=xxx text="What's your name?"
BOT:HandleTranscriptResults status=InterruptDetected
TTS:CancelText CallID=xxx
STT:Resume callid=xxx status=Resumed
BOT:HandleTranslationResults translated=你叫什么名字？
STT:Pause callid=xxx status=PausedWithInterrupt
TTS:AddText Event=TextStacked
TTS:listenForPlaybackDone Event=WakeFileSent
RTP:StreamFrom Event=WakeFile
STT:Resume callid=xxx status=Resumed
STT:heartbeat Event=SilentPacketSent - NormalMode

# 第三句
STT:MessageResponse Text="Goodbye"
BOT:HandleTranslationResults translated=再见
```

## 技术原理

### TTS文本堆栈机制
```go
// 检查是否有待处理的文本
if call.hasPendingText() || call.streaming {
    // 添加到文本堆栈
    call.addTextSlice(text)
    return
}
// 否则直接播放
tts.SendText(callid, text, id, language)
```

### 中断检测机制
```go
// 在暂停且中断启用状态下
if c.IsPaused() && c.IsInterruptEnabled() {
    // 只处理最终结果用于中断检测
    if isFinal && len(transcript) > 0 {
        // 发送中断信号
        c.transcriptCallback(c.callid, transcript, "interrupt")
    }
}
```

### 状态管理
```
正常模式 → 暂停模式 → 中断模式 → 恢复模式 → 正常模式
    ↓         ↓         ↓         ↓         ↓
发送心跳   跳过心跳   发送心跳   发送心跳   发送心跳
处理语音   跳过语音   检测打断   处理语音   处理语音
```

## 测试步骤

### 1. 基本连续对话测试
```bash
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json
export STT_LANGUAGE=en-US
export TTS_LANGUAGE=zh-CN
export TRANSLATE_SOURCE_LANGUAGE=en-US
export TRANSLATE_TARGET_LANGUAGE=zh-CN

go run main.go
```

### 2. 连续对话测试
1. **第一句**：用户说"Hello"
2. **等待翻译完成**：观察暂停/恢复机制
3. **第二句**：用户说"How are you?"
4. **等待翻译完成**：观察wakefile发送
5. **第三句**：用户说"What's your name?"
6. **验证**：第三句应该正常识别和翻译
7. **第四句**：用户说"Goodbye"
8. **验证**：第四句应该正常识别和翻译

### 3. 中断对话测试
1. **开始TTS播放**：系统播放翻译结果
2. **用户打断**：在TTS播放期间说话
3. **观察中断检测**：`STT:InterruptDetection`日志
4. **观察STT恢复**：`STT:Resume`日志
5. **继续对话**：用户说下一句话
6. **验证**：应该正常识别和翻译

### 4. 文本堆栈测试
1. **快速连续说话**：用户快速说多句话
2. **观察文本堆栈**：`TTS:AddText Event=TextStacked`日志
3. **观察wakefile发送**：每次TTS完成后都应该发送
4. **验证**：所有文本都应该正常播放

## 故障排除

### 问题1：第三句后仍然卡住
**原因**：wakefile信号没有正确发送
**解决**：检查`listenForPlaybackDone`方法中的wakefile发送逻辑

### 问题2：中断后无法继续对话
**原因**：STT没有正确恢复
**解决**：检查中断处理中的`ResumeCall`调用

### 问题3：文本堆栈后不播放
**原因**：deadfile状态没有重置
**解决**：检查wakefile信号的发送和接收

### 问题4：心跳状态错误
**原因**：暂停/恢复机制有问题
**解决**：检查STT的状态管理逻辑

## 总结

连续对话功能修复的核心优势：

1. **完整的对话流程**：支持多轮连续对话
2. **中断处理机制**：支持用户打断TTS播放
3. **文本堆栈管理**：正确处理快速连续语音
4. **状态同步**：TTS播放完成与音频流恢复的完美同步
5. **自然对话体验**：支持真实场景的连续对话

这个修复方案应该能彻底解决连续对话的问题，提供完整的对话功能。

## 关键修复点

### 1. TTS播放完成后总是发送wakefile
- **位置**：`google/google-tts.go`的`listenForPlaybackDone`方法
- **修复**：无论textStack状态如何，都发送wakefile信号
- **作用**：确保deadfile状态正确重置，音频流能够恢复

### 2. 中断后立即恢复STT
- **位置**：`voicebot/voicebot.go`的`HandleTranscriptResults`方法
- **修复**：在中断处理中添加`ResumeCall`调用
- **作用**：确保中断后STT能够正常接收用户语音

### 3. 状态管理优化
- **暂停机制**：TTS播放前暂停STT
- **中断检测**：TTS播放期间检测用户打断
- **恢复机制**：TTS播放完成后或中断后恢复STT
- **wakefile机制**：确保音频流状态正确重置

这个修复确保了连续对话的完整性和稳定性，支持真实场景的多轮对话。
