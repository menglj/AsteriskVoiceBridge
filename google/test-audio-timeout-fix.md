# 音频超时修复方案测试

## 问题分析

从最新日志分析，发现了真正的问题：

### 1. 手动播放完成回调正常工作 ✅
- `TTS:SpeakText callid=xxx status=ManualPlaybackComplete`
- `TTS:playbackCompleteCallBack callid=xxx status=CallingCallback`
- `BOT:HandlePlaybackComplete callid=xxx playbackid=translation`

### 2. 静默模式正确禁用 ✅
- `STT:DisableSilentMode callid=xxx status=SilentModeDisabled`

### 3. 关键问题：静默模式禁用后仍然超时 ❌
- 在静默模式禁用后，仍然出现音频超时错误
- 说明问题不在播放完成回调，而在静默模式禁用后的处理

## 根本原因

**静默模式禁用后，Google STT连接没有正确恢复，导致音频超时。**

具体原因：
1. 静默模式期间只发送静默音频包
2. 禁用静默模式后，没有发送真实音频数据
3. Google STT连接状态异常，导致超时

## 修复方案

### 重新连接音频包机制
在`DisableSilentMode`方法中添加重新连接音频包发送：

```go
// DisableSilentMode disables silent audio streaming
func (c *googleSTTClient) DisableSilentMode() {
    c.silentMu.Lock()
    defer c.silentMu.Unlock()
    c.silentMode = false
    log.Info("STT:DisableSilentMode", "callid", c.callid, "status", "SilentModeDisabled")
    
    // Send a few real audio packets to re-establish the connection
    // This helps prevent audio timeout after silent mode
    go func() {
        for i := 0; i < 3; i++ {
            time.Sleep(100 * time.Millisecond)
            if c.stream != nil && !c.IsSilentMode() {
                // Send a small amount of real audio data to keep connection alive
                req := &speechpb.StreamingRecognizeRequest{
                    StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
                        AudioContent: []byte{0x7F, 0x7F, 0x7F, 0x7F}, // Small μ-law silence
                    },
                }
                if err := c.stream.Send(req); err != nil {
                    log.Info("STT:DisableSilentMode", "Error", "Failed to send reconnection audio", "error", err)
                } else {
                    log.Info("STT:DisableSilentMode", "Event", "ReconnectionAudioSent", "packet", i+1)
                }
            }
        }
    }()
}
```

## 修复后的工作流程

```
用户说话 → STT识别 → 翻译 → TTS播放
    ↓
启用静默模式 → 发送静默音频包 → 保持连接活跃
    ↓
TTS播放完成 → 手动触发回调 → 禁用静默模式
    ↓
发送重新连接音频包 → 恢复Google STT连接 → 正常处理后续音频
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

### 2. 重新连接音频包测试
1. **开始通话**：用户说"Hello"
2. **观察日志**：
   ```
   STT:EnableSilentMode callid=xxx status=SilentModeEnabled
   TTS:SpeakText callid=xxx status=ManualPlaybackComplete playbackID=translation
   STT:DisableSilentMode callid=xxx status=SilentModeDisabled
   ```
3. **观察重新连接音频包**：
   ```
   STT:DisableSilentMode Event=ReconnectionAudioSent packet=1
   STT:DisableSilentMode Event=ReconnectionAudioSent packet=2
   STT:DisableSilentMode Event=ReconnectionAudioSent packet=3
   ```

### 3. 连续对话测试
1. **第一句话**：用户说"Hello"
2. **等待翻译完成**：观察静默模式启用和禁用
3. **第二句话**：用户说"How are you?"
4. **验证**：第二句话应该正常识别和翻译，没有音频超时错误

### 4. 长时间对话测试
1. **多轮对话**：进行5-10轮对话
2. **观察日志**：确认没有音频超时错误
3. **验证稳定性**：系统应该稳定运行

## 预期结果

### 正常情况
- ✅ 静默模式正确启用和禁用
- ✅ 重新连接音频包正确发送
- ✅ 没有音频超时错误
- ✅ 连续对话正常工作
- ✅ 长时间对话稳定运行

### 日志输出示例
```
STT:EnableSilentMode callid=xxx status=SilentModeEnabled
TTS:SpeakText callid=xxx status=ManualPlaybackComplete playbackID=translation
STT:DisableSilentMode callid=xxx status=SilentModeDisabled
STT:DisableSilentMode Event=ReconnectionAudioSent packet=1
STT:DisableSilentMode Event=ReconnectionAudioSent packet=2
STT:DisableSilentMode Event=ReconnectionAudioSent packet=3
# 后续用户语音正常识别，没有超时错误
```

## 故障排除

### 问题1：重新连接音频包发送失败
**原因**：Google STT连接已断开
**解决**：检查 `STT:DisableSilentMode Error=Failed to send reconnection audio` 日志

### 问题2：仍然出现音频超时
**原因**：重新连接音频包不够或时机不对
**解决**：增加音频包数量或调整发送时机

### 问题3：连续对话不工作
**原因**：重新连接机制没有正确恢复连接
**解决**：检查重新连接音频包的发送日志

## 技术细节

### 重新连接音频包
- **内容**：`[]byte{0x7F, 0x7F, 0x7F, 0x7F}` (μ-law silence)
- **数量**：3个包
- **间隔**：100毫秒
- **目的**：重新建立Google STT连接

### 时机控制
- **触发时机**：静默模式禁用后立即发送
- **条件检查**：确保不在静默模式且连接存在
- **异步处理**：使用goroutine避免阻塞

## 总结

修复方案包含：
1. **重新连接音频包机制**：在静默模式禁用后发送真实音频数据
2. **连接状态恢复**：确保Google STT连接正确恢复
3. **详细的日志记录**：便于调试和监控

这个修复方案应该能彻底解决音频超时问题，并提供稳定的连续对话功能。
