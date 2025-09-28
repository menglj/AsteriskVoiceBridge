# Deadfile状态重置修复

## 问题分析

从最新日志分析，发现了关键问题：

### 1. 暂停/恢复机制正常工作 ✅
- 暂停机制：`STT:Pause callid=xxx status=PausedWithInterrupt`
- 中断模式心跳：`STT:heartbeat Event="SilentPacketSent - InterruptMode"`
- 恢复机制：`STT:Resume callid=xxx status=Resumed`
- 正常模式心跳：`STT:heartbeat Event="SilentPacketSent - NormalMode"`

### 2. 关键问题：Deadfile状态未重置 ❌
- **TTS播放完成后**：`RTP:StreamFrom Event=EOF` 和 `deadfile = true`
- **音频流中断**：vproxy只发送静默包，不再转发真实音频
- **用户语音无法接收**：STT无法接收到用户新说的话

### 3. 根本原因
**TTS播放完成后，vproxy的`deadfile`状态没有重置，导致音频流中断**

## 解决方案：重置Deadfile状态

### 修复内容

#### 1. 在TTS播放完成后发送wakefile信号
```go
// TTS播放完成后重置deadfile状态
go func() {
    time.Sleep(100 * time.Millisecond) // 确保回调处理完成
    tts.wakefile <- true
    log.Info("TTS:listenForPlaybackDone", "Event", "WakeFileSent")
}()
```

#### 2. vproxy的wakefile处理机制
```go
case <-wakefile:
    if deadfile {
        log.Info("RTP:StreamFrom", "Event", "WakeFile")
        deadfile = false  // 重置deadfile状态
        filerevived <- true
    }
```

### 修复后的工作流程

```
用户说话 → STT识别 → 翻译 → 暂停STT → TTS播放
    ↓
TTS播放完成 → 发送wakefile → 重置deadfile → 恢复音频流
    ↓
恢复STT → 正常模式心跳 → 准备接收下一句用户语音
```

## 预期结果

### 正常情况
- ✅ TTS播放前：`STT:Pause callid=xxx status=PausedWithInterrupt`
- ✅ TTS播放期间：`STT:heartbeat Event=SilentPacketSent - InterruptMode`
- ✅ TTS播放完成：`TTS:listenForPlaybackDone Event=WakeFileSent`
- ✅ 音频流恢复：`RTP:StreamFrom Event=WakeFile`
- ✅ STT恢复：`STT:Resume callid=xxx status=Resumed`
- ✅ 正常模式：`STT:heartbeat Event=SilentPacketSent - NormalMode`
- ✅ 用户语音接收：`STT:Write callid=xxx status=NormalMode`

### 日志输出示例
```
STT:MessageResponse Text="How are you?"
BOT:HandleTranslationResults callid=xxx translated=你好吗？
STT:Pause callid=xxx status=PausedWithInterrupt
TTS:AddText CallID=xxx RawText=你好吗？
STT:heartbeat Event=SilentPacketSent - InterruptMode
TTS:Pipe Event=DataWriteComplete
TTS:listenForPlaybackDone Event=TextStackEmpty
TTS:listenForPlaybackDone Event=WakeFileSent
RTP:StreamFrom Event=WakeFile
STT:Resume callid=xxx status=Resumed
STT:heartbeat Event=SilentPacketSent - NormalMode
# 用户说第二句话时：
STT:Write callid=xxx status=NormalMode
STT:MessageResponse Text="I'm fine, thank you."
```

## 技术原理

### Deadfile状态管理
- **初始状态**：`deadfile = true`（等待音频流）
- **TTS播放**：`deadfile = false`（转发音频数据）
- **TTS完成**：`deadfile = true`（音频流结束）
- **重置状态**：发送`wakefile`信号 → `deadfile = false`（恢复音频流）

### 音频流控制逻辑
```go
if !pauseAudio && !deadfile && engaged {
    // 转发真实音频数据到STT
    n, err := r.Read(buf)
    // ... 处理音频数据
} else {
    // 发送静默包
    rtpPacket.SetPayload(cnPacket[:])
}
```

### Wakefile信号机制
```go
case <-wakefile:
    if deadfile {
        log.Info("RTP:StreamFrom", "Event", "WakeFile")
        deadfile = false  // 重置deadfile状态
        filerevived <- true
    }
```

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

### 2. Deadfile重置测试
1. **开始通话**：用户说"How are you?"
2. **观察TTS播放**：
   ```
   STT:Pause callid=xxx status=PausedWithInterrupt
   STT:heartbeat Event=SilentPacketSent - InterruptMode
   ```
3. **观察TTS完成**：
   ```
   TTS:listenForPlaybackDone Event=TextStackEmpty
   TTS:listenForPlaybackDone Event=WakeFileSent
   RTP:StreamFrom Event=WakeFile
   STT:Resume callid=xxx status=Resumed
   ```
4. **用户说第二句话**："I'm fine, thank you."
5. **验证**：应该看到`STT:Write`和`STT:MessageResponse`日志

### 3. 连续对话测试
1. **第一句话**：用户说"Hello"
2. **等待翻译完成**：观察deadfile重置
3. **第二句话**：用户说"How are you?"
4. **验证**：第二句话应该正常识别和翻译
5. **第三句话**：用户说"Goodbye"
6. **验证**：第三句话应该正常识别和翻译

### 4. 音频流状态验证
1. **TTS播放前**：`RTP:StreamFrom Event=StreamingSilence`
2. **TTS播放期间**：`RTP:StreamFrom Event=StreamingSpeech`
3. **TTS播放完成**：`RTP:StreamFrom Event=EOF`
4. **Wakefile发送**：`TTS:listenForPlaybackDone Event=WakeFileSent`
5. **音频流恢复**：`RTP:StreamFrom Event=WakeFile`
6. **用户语音接收**：`STT:Write callid=xxx status=NormalMode`

## 故障排除

### 问题1：没有WakeFileSent日志
**原因**：wakefile信号没有正确发送
**解决**：检查`listenForPlaybackDone`方法中的wakefile发送逻辑

### 问题2：没有WakeFile日志
**原因**：vproxy没有接收到wakefile信号
**解决**：检查wakefile通道的连接和传递

### 问题3：用户语音仍然无法识别
**原因**：deadfile状态没有正确重置
**解决**：检查vproxy的deadfile重置逻辑

### 问题4：音频流状态混乱
**原因**：wakefile信号发送时机不当
**解决**：调整wakefile发送的延迟时间

## 总结

Deadfile状态重置修复的核心优势：

1. **完整的音频流管理**：TTS播放 → 音频流中断 → 状态重置 → 音频流恢复
2. **连续对话支持**：TTS播放完成后可以立即接收用户新语音
3. **状态同步**：TTS播放完成与音频流恢复的完美同步
4. **自然对话体验**：支持多轮连续对话

这个修复方案应该能彻底解决TTS播放后无法识别用户新说话的问题，提供完整的连续对话功能。

## 技术细节

### 关键修复点
1. **TTS播放完成检测**：`listenForPlaybackDone`方法中的`deadfile`信号处理
2. **状态重置机制**：发送`wakefile`信号重置vproxy的`deadfile`状态
3. **时序控制**：100ms延迟确保回调处理完成后再重置状态
4. **日志追踪**：添加`WakeFileSent`日志便于调试

### 状态转换图
```
TTS播放开始 → 音频流活跃 → TTS播放完成 → 音频流中断 → 发送wakefile → 音频流恢复
     ↓           ↓           ↓           ↓           ↓           ↓
deadfile=false 转发音频    deadfile=true  只发静默    wakefile信号  deadfile=false
```

这个修复确保了TTS播放完成后音频流能够正确恢复，从而支持连续对话功能。
