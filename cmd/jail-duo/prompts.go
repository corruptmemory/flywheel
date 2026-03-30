package main

const specContent = `# Spec: Hello Service
Build a CLI tool that prints a greeting.
The greeting should include the user's name.
`

const builderPrompt = `# Builder Instructions

You are the Builder agent. Read spec.md, notice the ambiguity, ask the Product Owner for clarification, then implement.

1. Read spec.md in this workspace.

2. The spec doesn't say where the user's name comes from. Send a PLEASE CLARIFY request to the Product Owner:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_request","arguments":{"type":"PLEASE CLARIFY","content":"The spec says the greeting should include the users name, but does not specify where the name comes from. Should it be a CLI argument, environment variable, or read from stdin?"}}}'

3. Wait 15 seconds for the PO to respond, then check:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"check_response","arguments":{"request_id":"000001"}}}'

4. If the response is not ready, wait another 15 seconds and try step 3 again. Repeat up to 5 times.

5. Once you have the PO response, implement the spec accordingly. Create a file called hello.go with the implementation.

6. Signal completion:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"write_done","arguments":{"status":"PASS","details":"Implemented hello.go per PO clarification."}}}'

7. Report what happened.

Do not ask questions. Execute these steps.
`

const poPrompt = `# Product Owner Instructions

You are the Product Owner. Your job is to wait for and respond to requests from other agents.

1. Poll for pending requests:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_pending_requests","arguments":{}}}'

2. If no requests yet (the result content text shows an empty list), wait 10 seconds and repeat step 1. Keep polling until you get a request. Try up to 12 times (2 minutes total).

3. When you see a request, read it:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_request","arguments":{"request_id":"000001"}}}'

4. Send your response — the user's name should come from a CLI argument:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"send_response","arguments":{"request_id":"000001","content":"The users name should be provided as a CLI argument. Usage: ./hello <name>. If no argument is given, print a usage message to stderr and exit with code 1."}}}'

5. Signal completion:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"write_done","arguments":{"status":"PASS","details":"Responded to all requests."}}}'

6. Report what happened.

Do not ask questions. Execute these steps.
`
