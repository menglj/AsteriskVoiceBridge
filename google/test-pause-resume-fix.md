# 暂停/恢复机制修复

## 问题分析

从最新日志分析，发现了关键问题：

### 1. 心跳机制正常工作 ✅
- 心跳正常发送：`STT:heartbeat Event="SilentPacketSent - NormalMode"`
- 连接保持活跃，没有音频超时错误

### 2. 关键问题：缺少暂停/恢复机制 ❌
- **TTS播放期间**：心跳仍在正常模式发送
- **无法检测用户打断**：没有进入中断模式
- **用户说话无法识别**：TTS播放后用户新说的话无法识别

### 3. 根本原因
**在TTS播放期间没有调用暂停机制，导致无法检测用户打断**

## 解决方案：添加暂停/恢复机制

### 修复内容

#### 1. 在TTS播放前暂停STT
```go
// HandleTranslationResults 和 SendText 方法中
// Pause STT during TTS playback to enable interrupt detection
if useGoogle {
    if v.googleSTTProvider != nil {
        v.googleSTTProvider.PauseCall(callid)
    }
}
```

#### 2. 在TTS播放完成后恢复STT
```go
// HandlePlaybackComplete 方法中
// Resume STT after TTS playback is complete
if useGoogle {
    if v.googleSTTProvider != nil {
        v.googleSTTProvider.ResumeCall(callid)
    }
}
```

### 修复后的工作流程

```
用户说话 → STT识别 → 翻译 → 暂停STT → TTS播放
    ↓
中断模式心跳 → 检测用户语音 → 取消TTS → 恢复STT
    ↓
TTS播放完成 → 恢复STT → 正常模式心跳 → 准备下一句
```

## 预期结果

### 正常情况
- ✅ TTS播放前：`STT:Pause callid=xxx status=PausedWithInterrupt`
- ✅ TTS播放期间：`STT:heartbeat Event=SilentPacketSent - InterruptMode`
- ✅ 用户打断：`STT:InterruptDetection callid=xxx text="用户语音"`
- ✅ TTS播放完成：`STT:Resume callid=xxx status=Resumed`
- ✅ 正常模式：`STT:heartbeat Event=SilentPacketSent - NormalMode`

### 日志输出示例
```
STT:MessageResponse Text="Hello, how are you?"
BOT:HandleTranslationResults callid=xxx translated=你好吗？
STT:Pause callid=xxx status=PausedWithInterrupt
TTS:AddText CallID=xxx RawText=你好吗？
STT:heartbeat Event=SilentPacketSent - InterruptMode
# 用户说话时：
STT:InterruptDetection callid=xxx text="用户语音"
BOT:HandleTranscriptResults callid=xxx text="用户语音" level=interrupt
TTS:SpeakText callid=xxx status=ManualPlaybackComplete
STT:Resume callid=xxx status=Resumed
STT:heartbeat Event=SilentPacketSent - NormalMode
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

### 2. 暂停/恢复机制测试
1. **开始通话**：用户说"Hello, how are you?"
2. **观察暂停**：
   ```
   STT:Pause callid=xxx status=PausedWithInterrupt
   STT:heartbeat Event=SilentPacketSent - InterruptMode
   ```
3. **TTS播放期间**：用户说"No"
4. **观察中断检测**：
   ```
   STT:InterruptDetection callid=xxx text="No"
   BOT:HandleTranscriptResults callid=xxx text="No" level=interrupt
   ```
5. **TTS播放完成**：
   ```
   STT:Resume callid=xxx status=Resumed
   STT:heartbeat Event=SilentPacketSent - NormalMode
   ```

### 3. 连续对话测试
1. **第一句话**：用户说"Hello"
2. **等待翻译完成**：观察暂停机制
3. **第二句话**：用户说"How are you?"
4. **验证**：第二句话应该正常识别和翻译

### 4. 打断功能测试
1. **开始TTS播放**：系统播放翻译结果
2. **用户打断**：在TTS播放期间说话
3. **验证**：应该检测到打断，取消TTS，处理用户语音

## 技术原理

### 暂停/恢复机制
- **暂停状态**：`paused = true, interruptEnabled = true`
- **中断模式**：发送心跳包检测用户语音
- **恢复状态**：`paused = false, interruptEnabled = false`
- **正常模式**：发送心跳包保持连接

### 状态转换
```
正常模式 → 暂停模式 → 中断模式 → 恢复模式 → 正常模式
    ↓         ↓         ↓         ↓         ↓
发送心跳   跳过心跳   发送心跳   发送心跳   发送心跳
处理语音   跳过语音   检测打断   处理语音   处理语音
```

### 中断检测逻辑
```go
// 在暂停且中断启用状态下
if c.IsPaused() && c.IsInterruptEnabled() {
    // 只处理最终结果用于中断检测
    if isFinal && len(transcript) > 0 {
        log.Info("STT:InterruptDetection", "callid", c.callid, "text", transcript)
        // 发送中断信号到voicebot
        c.transcriptCallback(c.callid, transcript, "interrupt")
    }
}
```

## 故障排除

### 问题1：没有暂停日志
**原因**：暂停机制没有正确调用
**解决**：检查`HandleTranslationResults`和`SendText`方法中的暂停调用

### 问题2：没有中断检测
**原因**：中断模式没有正确启用
**解决**：检查`Pause()`方法是否正确设置`interruptEnabled = true`

### 问题3：TTS播放后无法识别
**原因**：恢复机制没有正确调用
**解决**：检查`HandlePlaybackComplete`方法中的恢复调用

## 总结

暂停/恢复机制修复的核心优势：

1. **完整的状态管理**：正常模式 ↔ 暂停模式 ↔ 中断模式
2. **中断检测功能**：TTS播放期间可以检测用户打断
3. **连接稳定性**：心跳机制保持Google STT连接活跃
4. **自然对话体验**：支持连续对话和打断功能

这个修复方案应该能彻底解决TTS播放后无法识别用户新说话的问题，提供完整的对话功能。
