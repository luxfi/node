import re

# Read the file
with open('vms/platformvm/vm_test.go', 'r') as f:
    content = f.read()

# Pattern to find signer.NewProofOfPossession(sk) in function arguments
pattern = r'(\n\t+)(signer\.NewProofOfPossession\(sk\))(,)'

# Counter for replacements
counter = 0

def replace_pop(match):
    global counter
    counter += 1
    indent = match.group(1)
    pop_var = f'pop{counter}'
    
    # Return the replacement - we'll insert the pop generation before the function call
    return f'{indent}{pop_var},'

# Find the function calls that contain these
lines = content.split('\n')
new_lines = []
i = 0
pop_counter = 0

while i < len(lines):
    line = lines[i]
    
    # Check if this line contains signer.NewProofOfPossession
    if 'signer.NewProofOfPossession(sk)' in line:
        pop_counter += 1
        pop_var = f'pop{pop_counter}'
        
        # Find the beginning of this statement (look back for builder.New or similar)
        j = i - 1
        while j >= 0 and not ('builder.New' in lines[j] or 'wallet.' in lines[j]):
            j -= 1
        
        if j >= 0:
            # Insert the pop generation before the statement
            # Find the proper indentation
            indent = '\t'
            if lines[j].startswith('\t'):
                indent = re.match(r'^(\t+)', lines[j]).group(1)
            
            # Add blank line and pop generation
            new_lines.append('')
            new_lines.append(f'{indent}// Generate proof of possession')
            new_lines.append(f'{indent}{pop_var}, err := signer.NewProofOfPossession(sk)')
            new_lines.append(f'{indent}require.NoError(err)')
            
            # Add the lines up to and including the current line, replacing the pop call
            for k in range(j, i):
                new_lines.append(lines[k])
            
            # Replace the signer.NewProofOfPossession(sk) with pop_var
            new_lines.append(line.replace('signer.NewProofOfPossession(sk)', pop_var))
        else:
            new_lines.append(line)
    else:
        new_lines.append(line)
    
    i += 1

# Write the file back
with open('vms/platformvm/vm_test.go', 'w') as f:
    f.write('\n'.join(new_lines))

print(f"Fixed {pop_counter} proof of possession calls")
