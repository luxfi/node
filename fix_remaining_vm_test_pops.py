import re

# Read the file
with open('vms/platformvm/vm_test.go', 'r') as f:
    lines = f.readlines()

# Find remaining occurrences
remaining = []
for i, line in enumerate(lines):
    if 'signer.NewProofOfPossession(sk)' in line:
        remaining.append(i + 1)  # 1-indexed line numbers

print(f"Remaining occurrences at lines: {remaining}")

# Fix each occurrence
for line_num in reversed(remaining):  # Process in reverse to maintain line numbers
    idx = line_num - 1  # Convert to 0-indexed
    
    # Find the sk declaration before this
    sk_line = -1
    for i in range(idx - 1, max(0, idx - 30), -1):
        if 'sk, err := bls.NewSecretKey()' in lines[i]:
            sk_line = i
            break
    
    if sk_line != -1:
        # Find the require.NoError after sk declaration
        insert_idx = -1
        for i in range(sk_line + 1, min(sk_line + 5, len(lines))):
            if 'require.NoError(err)' in lines[i]:
                insert_idx = i + 1
                break
        
        if insert_idx != -1:
            # Insert the pop generation
            lines.insert(insert_idx, '\n')
            lines.insert(insert_idx + 1, '\t// Generate proof of possession\n')
            lines.insert(insert_idx + 2, '\tpop, err := signer.NewProofOfPossession(sk)\n')
            lines.insert(insert_idx + 3, '\trequire.NoError(err)\n')
            
            # Update the line that uses it (adjust for insertions)
            new_idx = idx + 4
            lines[new_idx] = lines[new_idx].replace('signer.NewProofOfPossession(sk)', 'pop')

# Write back
with open('vms/platformvm/vm_test.go', 'w') as f:
    f.writelines(lines)

print("Fixed remaining proof of possession calls")
