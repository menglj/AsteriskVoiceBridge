# 无心跳机制测试

## 测试目的

验证Google STT是否能够像Deepgram一样，仅依赖Asterisk的`sound:longsilence`保持连接活跃，而不需要额外的心跳机制。

## 测试方案

### 1. 移除心跳机制
- 修改`heartbeat()`函数，不再发送静默包
- 仅记录调试日志：`STT:heartbeat Event=Skipped - TestingNoHeartbeat`
- 完全依赖Asterisk的`sound:longsilence`和vproxy的音频流

### 2. 保持其他机制不变
- 保持`Write()`方法正常发送真实音频数据
- 保持暂停/恢复机制用于中断检测
- 保持TTS播放完成回调机制

## 预期结果

### 成功情况
- ✅ 没有心跳包发送日志
- ✅ 持续接收真实音频数据
- ✅ 没有音频超时错误
- ✅ 连续对话正常工作

### 失败情况
- ❌ 出现`Audio Timeout Error`
- ❌ 第二句话无法识别
- ❌ 连接在静默期间断开

## 测试步骤

### 1. 启动系统
```bash
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json
export STT_LANGUAGE=en-US
export TTS_LANGUAGE=zh-CN
export TRANSLATE_SOURCE_LANGUAGE=en-US
export TRANSLATE_TARGET_LANGUAGE=zh-CN

go run main.go
```

### 2. 观察日志
1. **启动阶段**：
   ```
   STT:heartbeat Event=HeartbeatStarted interval=10s
   ```

2. **心跳期间**：
   ```
   STT:heartbeat Event=Skipped - TestingNoHeartbeat
   ```

3. **用户说话**：
   ```
   STT:Write callid=xxx status=AudioSent dataLen=160
   STT:MessageResponse Text="Hello"
   ```

4. **TTS播放**：
   ```
   TTS:SpeakText callid=xxx status=ManualPlaybackComplete
   BOT:HandlePlaybackComplete callid=xxx playbackid=translation
   ```

5. **关键测试点**：观察TTS播放完成后是否出现音频超时

### 3. 连续对话测试
1. **第一句话**：用户说"Hello"
2. **等待翻译完成**：观察是否有音频超时
3. **第二句话**：用户说"How are you?"
4. **验证结果**：第二句话是否正常识别

## 技术分析

### 如果测试成功
**说明**：Google STT可以像Deepgram一样，仅依赖Asterisk的`sound:longsilence`保持连接活跃。

**原因分析**：
- Asterisk的`sound:longsilence`提供连续的静默音频流
- vproxy的`StreamFrom`方法持续发送音频数据
- Google STT能够自然处理连续的音频流，包括静默音频

**后续方案**：
- 完全移除心跳机制
- 简化代码，提高稳定性
- 与Deepgram保持一致的架构

### 如果测试失败
**说明**：Google STT需要额外的心跳机制来保持连接活跃。

**原因分析**：
- Google STT对静默期间更敏感
- 需要定期发送音频数据防止超时
- 与Deepgram的处理机制不同

**后续方案**：
- 恢复心跳机制
- 优化心跳间隔和静默包内容
- 保持当前的心跳保活方案

## 对比分析

| 方案 | Deepgram | Google STT (测试) | Google STT (心跳) |
|------|----------|-------------------|-------------------|
| 心跳机制 | 无 | 无 | 有 |
| 依赖机制 | sound:longsilence | sound:longsilence | sound:longsilence + 心跳 |
| 代码复杂度 | 简单 | 简单 | 中等 |
| 连接稳定性 | 高 | 待测试 | 高 |
| 维护成本 | 低 | 低 | 中等 |

## 故障排除

### 问题1：立即出现音频超时
**原因**：Google STT需要更频繁的音频数据
**解决**：恢复心跳机制，缩短心跳间隔

### 问题2：TTS播放后出现超时
**原因**：TTS播放期间音频流中断
**解决**：检查vproxy的音频流处理机制

### 问题3：第二句话无法识别
**原因**：连接在静默期间断开
**解决**：恢复心跳机制或优化音频流处理

## 总结

这个测试将验证Google STT是否能够像Deepgram一样，仅依赖Asterisk的`sound:longsilence`保持连接活跃。如果测试成功，我们可以简化代码架构，提高系统稳定性。如果测试失败，我们需要保持心跳机制，并进一步优化。
