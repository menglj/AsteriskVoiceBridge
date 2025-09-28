# Google STT 静默音频方案测试

## 概述

基于Deepgram STT的实现方式，我们为Google STT实现了静默音频流方案，模拟Deepgram的持续音频流处理方式。

## Deepgram vs Google STT 对比

### Deepgram STT 方式
- **持续音频流**：`udpproxy.StreamTo(dgClient)` 持续发送音频
- **静默音频**：在TTS播放期间，vproxy自动发送静默音频包
- **VAD检测**：通过VAD机制检测用户语音活动
- **无暂停机制**：音频流持续运行，不会暂停

### Google STT 新方案
- **静默模式**：在TTS播放期间启用静默模式
- **静默音频包**：发送预生成的静默音频包而不是真实音频
- **持续连接**：保持Google STT连接活跃，防止超时
- **自然中断**：用户说话时自然检测到中断

## 静默音频包生成

### μ-law 编码
```go
// μ-law silence (0x7F for μ-law silence)
size := vproxy.GetRTP_PAYLOAD_SIZE()
silentPacket := make([]byte, size)
for i := range silentPacket {
    silentPacket[i] = 0x7F // μ-law silence value
}
```

### Linear 16-bit 编码
```go
// Linear 16-bit silence (all zeros)
size := vproxy.GetRTP_PAYLOAD_SIZE()
return make([]byte, size)
```

## 工作流程

### 1. 正常语音识别
```
用户说话 → 真实音频 → Google STT → 转录结果 → 翻译 → TTS播放
```

### 2. TTS播放期间
```
TTS播放 → 启用静默模式 → 发送静默音频包 → 保持STT连接活跃
```

### 3. 用户中断
```
用户说话 → 检测到语音 → 取消TTS → 禁用静默模式 → 正常处理
```

## 测试步骤

### 1. 基本功能测试
```bash
# 设置环境变量
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json
export STT_LANGUAGE=en-US
export TTS_LANGUAGE=zh-CN
export TRANSLATE_SOURCE_LANGUAGE=en-US
export TRANSLATE_TARGET_LANGUAGE=zh-CN

# 启动系统
go run main.go
```

### 2. 静默模式测试
1. **开始通话**：用户说"Hello"
2. **观察日志**：
   ```
   STT:EnableSilentMode callid=xxx status=SilentModeEnabled
   STT:Write callid=xxx status=SilentMode - SendingSilentAudio
   ```
3. **TTS播放**：系统播放翻译结果
4. **观察日志**：
   ```
   STT:Write callid=xxx status=SilentMode - SendingSilentAudio
   STT:heartbeat Event=SilentPacketSent - InterruptMode
   ```

### 3. 中断检测测试
1. **TTS播放期间**：用户说"Stop"
2. **观察日志**：
   ```
   STT:InterruptDetection callid=xxx text=Stop
   BOT:HandleTranscriptResults callid=xxx status=InterruptDetected text=Stop
   STT:DisableSilentMode callid=xxx status=SilentModeDisabled
   ```
3. **验证**：TTS停止，开始处理新的用户语音

### 4. 音频超时测试
1. **长时间TTS播放**：播放较长的翻译文本
2. **观察日志**：
   ```
   STT:heartbeat Event=SilentPacketSent - InterruptMode
   STT:Write callid=xxx status=SilentMode - SendingSilentAudio
   ```
3. **验证**：没有"Audio Timeout Error"错误

## 预期行为

### 正常情况
- ✅ 用户语音正常识别和翻译
- ✅ TTS播放期间发送静默音频包
- ✅ 没有音频超时错误
- ✅ 用户中断正常工作

### 日志输出
```
STT:EnableSilentMode callid=xxx status=SilentModeEnabled
STT:Write callid=xxx status=SilentMode - SendingSilentAudio
STT:heartbeat Event=SilentPacketSent - InterruptMode
STT:InterruptDetection callid=xxx text=用户语音
STT:DisableSilentMode callid=xxx status=SilentModeDisabled
```

## 优势

1. **模拟Deepgram**：采用与Deepgram相同的持续音频流方式
2. **防止超时**：静默音频包保持连接活跃
3. **自然中断**：用户说话时自然检测到中断
4. **简化逻辑**：不需要复杂的暂停/恢复机制
5. **稳定可靠**：减少状态管理的复杂性

## 配置选项

### 环境变量
- `USE_GOOGLE_STT_TTS=true`：启用Google STT/TTS
- `GOOGLE_STT_HEARTBEAT_INTERVAL=5s`：心跳间隔（可选）

### 静默模式控制
- `EnableSilentMode(callid)`：启用静默模式
- `DisableSilentMode(callid)`：禁用静默模式
- `IsSilentMode()`：检查静默模式状态

## 故障排除

### 问题1：仍然出现音频超时
**原因**：静默音频包格式不正确
**解决**：检查`getSilentAudioPacket()`函数的编码格式

### 问题2：中断检测不工作
**原因**：静默模式下没有处理用户语音
**解决**：确保中断检测逻辑正确处理静默模式

### 问题3：TTS播放不停止
**原因**：中断检测没有正确取消TTS
**解决**：检查`HandleTranscriptResults`中的中断处理逻辑

## 总结

静默音频方案成功模拟了Deepgram的持续音频流方式，解决了Google STT的音频超时问题，同时保持了自然的中断功能。这种方案更简单、更稳定，提供了更好的用户体验。
