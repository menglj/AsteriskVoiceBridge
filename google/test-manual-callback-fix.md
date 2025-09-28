# 手动播放完成回调修复

## 问题分析

从最新日志分析，发现了关键问题：

### 1. 问题：手动播放完成回调没有发送wakefile ❌
- **TTS播放完成**：`TTS:SpeakText callid=xxx status=ManualPlaybackComplete`
- **播放完成回调**：`TTS:playbackCompleteCallBack callid=xxx status=CallingCallback`
- **STT恢复**：`STT:Resume callid=xxx status=Resumed`
- **正常模式心跳**：`STT:heartbeat Event="SilentPacketSent - NormalMode"`
- **缺少wakefile**：没有`TTS:playbackCompleteCallBack Event=WakeFileSent`日志

### 2. 根本原因
**手动播放完成回调（`playbackCompleteCallBack`）没有发送wakefile信号，导致deadfile状态未重置**

### 3. 结果
- **音频流中断**：vproxy的deadfile状态没有重置
- **用户语音无法接收**：STT无法接收到用户新说的话
- **只能识别第一句**：后续语音无法识别

## 解决方案

### 修复：在手动播放完成回调中发送wakefile

#### 问题原因
```go
// 原来的逻辑：手动播放完成回调没有发送wakefile
func (tts *ttsCall) playbackCompleteCallBack() {
    // 调用回调
    tts.playbackCompleteCB(tts.callid, tts.currentPlaybackID)
    // 但没有发送wakefile信号
}
```

#### 修复方案
```go
// 修复后：手动播放完成回调也发送wakefile
func (tts *ttsCall) playbackCompleteCallBack() {
    // 调用回调
    tts.playbackCompleteCB(tts.callid, tts.currentPlaybackID)
    
    // 总是重置deadfile状态
    go func() {
        time.Sleep(100 * time.Millisecond)
        tts.wakefile <- true
        log.Info("TTS:playbackCompleteCallBack", "Event", "WakeFileSent")
    }()
}
```

## 修复后的工作流程

### 正常TTS播放流程
```
用户说话 → STT识别 → 翻译 → 暂停STT → TTS播放
    ↓
TTS播放完成 → 手动播放完成回调 → 发送wakefile → 重置deadfile → 恢复STT
    ↓
正常模式心跳 → 准备接收下一句用户语音
```

### 预期日志输出
```
STT:MessageResponse Text="Hello"
BOT:HandleTranslationResults translated=你好
STT:Pause callid=xxx status=PausedWithInterrupt
TTS:AddText Event=TextSent
STT:heartbeat Event=SilentPacketSent - InterruptMode
TTS:SpeakText callid=xxx status=ManualPlaybackComplete
TTS:playbackCompleteCallBack callid=xxx status=CallingCallback
TTS:playbackCompleteCallBack Event=WakeFileSent
RTP:StreamFrom Event=WakeFile
STT:Resume callid=xxx status=Resumed
STT:heartbeat Event=SilentPacketSent - NormalMode
# 用户说第二句话时：
STT:Write callid=xxx status=NormalMode
STT:MessageResponse Text="How are you?"
```

## 技术原理

### 手动播放完成回调机制
```go
// 在SpeakText方法中
go func() {
    time.Sleep(2 * time.Second) // 等待音频处理完成
    if tts.currentPlaybackID != "" {
        log.Info("TTS:SpeakText", "status", "ManualPlaybackComplete")
        tts.playbackCompleteCallBack() // 手动触发回调
    }
}()
```

### 播放完成回调处理
```go
func (tts *ttsCall) playbackCompleteCallBack() {
    // 调用voicebot的回调
    if tts.playbackCompleteCB != nil {
        tts.playbackCompleteCB(tts.callid, tts.currentPlaybackID)
    }
    
    // 发送wakefile信号重置deadfile状态
    go func() {
        time.Sleep(100 * time.Millisecond)
        tts.wakefile <- true
        log.Info("TTS:playbackCompleteCallBack", "Event", "WakeFileSent")
    }()
}
```

### 双重保障机制
1. **deadfile信号**：vproxy检测到音频流结束
2. **手动回调**：2秒延迟确保播放完成
3. **wakefile信号**：重置deadfile状态，恢复音频流

## 测试步骤

### 1. 基本功能测试
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
2. **观察TTS播放**：
   ```
   STT:Pause callid=xxx status=PausedWithInterrupt
   STT:heartbeat Event=SilentPacketSent - InterruptMode
   ```
3. **观察TTS完成**：
   ```
   TTS:SpeakText callid=xxx status=ManualPlaybackComplete
   TTS:playbackCompleteCallBack Event=WakeFileSent
   RTP:StreamFrom Event=WakeFile
   STT:Resume callid=xxx status=Resumed
   ```
4. **用户说第二句话**："How are you?"
5. **验证**：应该看到`STT:Write`和`STT:MessageResponse`日志

### 3. 音频流状态验证
1. **TTS播放前**：`RTP:StreamFrom Event=StreamingSilence`
2. **TTS播放期间**：`RTP:StreamFrom Event=StreamingSpeech`
3. **TTS播放完成**：`TTS:SpeakText status=ManualPlaybackComplete`
4. **Wakefile发送**：`TTS:playbackCompleteCallBack Event=WakeFileSent`
5. **音频流恢复**：`RTP:StreamFrom Event=WakeFile`
6. **用户语音接收**：`STT:Write callid=xxx status=NormalMode`

## 故障排除

### 问题1：没有WakeFileSent日志
**原因**：手动播放完成回调没有正确发送wakefile
**解决**：检查`playbackCompleteCallBack`方法中的wakefile发送逻辑

### 问题2：用户语音仍然无法识别
**原因**：deadfile状态没有正确重置
**解决**：检查vproxy的wakefile接收和deadfile重置逻辑

### 问题3：音频流状态混乱
**原因**：wakefile信号发送时机不当
**解决**：调整wakefile发送的延迟时间

### 问题4：TTS播放完成检测失败
**原因**：手动回调的2秒延迟不够
**解决**：增加延迟时间或改进检测机制

## 总结

手动播放完成回调修复的核心优势：

1. **完整的播放完成检测**：手动回调确保TTS播放完成被正确检测
2. **deadfile状态重置**：wakefile信号确保音频流状态正确重置
3. **连续对话支持**：TTS播放完成后可以立即接收用户新语音
4. **双重保障机制**：deadfile信号和手动回调的双重保障
5. **自然对话体验**：支持多轮连续对话

这个修复方案应该能彻底解决只能识别第一句的问题，提供完整的连续对话功能。

## 关键修复点

### 1. 手动播放完成回调中发送wakefile
- **位置**：`google/google-tts.go`的`playbackCompleteCallBack`方法
- **修复**：在手动播放完成回调中添加wakefile发送逻辑
- **作用**：确保deadfile状态正确重置，音频流能够恢复

### 2. 双重保障机制
- **deadfile信号**：vproxy检测到音频流结束
- **手动回调**：2秒延迟确保播放完成
- **wakefile信号**：重置deadfile状态，恢复音频流

### 3. 状态管理优化
- **播放完成检测**：手动回调确保检测准确性
- **状态重置**：wakefile信号确保状态正确重置
- **音频流恢复**：deadfile状态重置后音频流能够恢复

这个修复确保了TTS播放完成后音频流能够正确恢复，从而支持连续对话功能。
