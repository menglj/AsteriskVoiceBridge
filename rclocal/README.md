This module handles the communication with a studio application via POST method.

At the initial stage of the call, the application will request a configuration that follows this format.  When the AI chooses a tool with parameter, the bot will send a POST to the url associated with that function including any AI chosen parameters as well as any `pass_back` variables received as a result of the previous POST.

The `commands` are instructions to be executed by the bot.  Individual commands are documented below.

The `instructions` and `tool` objects are passed through to OpenAI and configure how the Agent acts.

## Response JSON:
```json
{
    "commands": [{
        "command": "command name",    // command for the bot to execute
        "parameters": {               // string value pairs specific to the command, optional
            "key1": "val1",
            "key2": "val2"
        },
        "wait": "int"                 // number of seconds to wait before executing command, optional. Waited commands are cleared upon tool selection.
    }],
    "instructions": "instructions string",  // Raw instructions to pass to AI model
    "tools": [                              // Set of tools for the AI to choose from.
        {
            "dtmf_equivalent": "digit as string",   // Optional single digit to indicate this choice, bypasses AI and includes no parameters
            "url": "url string",                    // callback url to invoke via POST
            "tool": {
                "type": "function",
                "name": "name string",                  // reference name for the function
                "description": "description string",    // description of the function to help the ai
                "parameters": {
                    "type": "object",
                    "properties": {
                        "string": {
                            "type": "string",
                            "description": "description string",    // parameter description
                            "enum": [
                                "choice1",
                                "choice2"
                            ]
                        }
                    }
                },
                "pass_back": {                          // key, value pairs to pass back with the POST
                    "key1": "val1",
                    "key2": "val2"
                }
            }
        }
    ]
}
```

## Bot Commands

* All Commands may include a "wait" flag, indicating the command should be executed after the set number of seconds.  If not set, the command will be executed immediately.  Any commands being waited on are cancelled upon tool selection (either by ai or dtmf.) Example:
    ```json
    "wait" : "10"
    ```



### Engage AI

Disengage Text/Audio to and from the AI.

```json
{
    "command": "engage_ai"
}
```
Parameters: None

### Disengage AI

Disengage Text/Audio to and from the AI.

```json
{
    "command": "disengage_ai"
}
```
Parameters: None

### Hang Up Call

Terminate the call and hang up on the caller.

```json
{
    "command": "hang_up_call"
}
```
Parameters: None

### Play File

Play a file on the caller channel
```json
{
    "command": "play_file",
    "parameters": {"file":"thanks_goodbye", "language":"en"}
}
```
Parameters:
* "file": name of file
* "language": playback language

### Dial Plan

Add a new Channel to the call, hairpinning via Asterisk

```json
{
    "command": "dial_plan",
    "parameters": {"host": "context", "userinfo": "priority"}
}
```
Parameters:
* "host": Asterisk dialplan context
* "userinfo": Asterisk dialplan priority