import re

# Read the file
with open('vms/platformvm/vm_regression_test.go', 'r') as f:
    lines = f.readlines()

# Lines to fix (adjusted after first fix)
fixes = [
    (1945, 'sk2'),  # Line 1941 became 1945 after adding 4 lines
    (2149, 'sk2'),  # Line 2141 became 2149 after adding 4 lines
    (2367, 'sk2'),  # Line 2355 became 2367 after adding 4 lines (and more from previous fixes)
]

# Process each fix
for line_num, sk_var in fixes:
    # Adjust line number (1-indexed to 0-indexed)
    idx = line_num - 1
    
    if idx < len(lines) and 'signer.NewProofOfPossession' in lines[idx]:
        # Find where to insert the proof generation
        # Look backwards for the sk2 declaration
        insert_idx = idx
        for i in range(idx - 1, max(0, idx - 20), -1):
            if f'{sk_var}, err := bls.NewSecretKey()' in lines[i]:
                # Insert after the require.NoError(err) line
                for j in range(i + 1, min(i + 5, len(lines))):
                    if 'require.NoError(err)' in lines[j]:
                        insert_idx = j + 1
                        break
                break
        
        # Insert the new lines
        pop_var = f'pop{sk_var[2:]}'  # pop2 from sk2
        new_lines = [
            f'\n',
            f'\t// Generate proof of possession\n',
            f'\t{pop_var}, err := signer.NewProofOfPossession({sk_var})\n',
            f'\trequire.NoError(err)\n'
        ]
        
        # Insert the lines
        for i, new_line in enumerate(new_lines):
            lines.insert(insert_idx + i, new_line)
        
        # Update the line that uses NewProofOfPossession
        # Adjust index due to insertions
        new_idx = idx + len(new_lines)
        if new_idx < len(lines):
            lines[new_idx] = lines[new_idx].replace(f'signer.NewProofOfPossession({sk_var})', pop_var)

# Write the file back
with open('vms/platformvm/vm_regression_test.go', 'w') as f:
    f.writelines(lines)

print("Fixed remaining proof of possession calls")
