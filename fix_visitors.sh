#!/bin/bash

# Find all files that implement Visitor interface and add the missing Net methods

# Files to update based on the build errors
files=(
  "/Users/z/work/lux/node/vms/platformvm/txs/fee/complexity.go"
  "/Users/z/work/lux/node/vms/platformvm/txs/fee/static_calculator.go"  
  "/Users/z/work/lux/node/wallet/chain/p/signer/visitor.go"
  "/Users/z/work/lux/node/vms/platformvm/metrics/tx_metrics.go"
)

# Add the missing methods to each visitor implementation
for file in "${files[@]}"; do
  echo "Processing $file..."
  
  # Check if file contains complexityVisitor
  if grep -q "complexityVisitor" "$file"; then
    # Add missing methods for complexityVisitor
    cat >> "$file" << 'EOF'

func (c *complexityVisitor) AddNetValidatorTx(tx *txs.AddNetValidatorTx) error {
	c.output = IntrinsicAddNetValidatorTxComplexities
	return nil
}

func (c *complexityVisitor) CreateNetTx(tx *txs.CreateNetTx) error {
	c.output = IntrinsicCreateNetTxComplexities
	return nil
}

func (c *complexityVisitor) RemoveNetValidatorTx(tx *txs.RemoveNetValidatorTx) error {
	c.output = IntrinsicRemoveNetValidatorTxComplexities
	return nil
}

func (c *complexityVisitor) TransformNetTx(tx *txs.TransformNetTx) error {
	c.output = IntrinsicTransformNetTxComplexities
	return nil
}

func (c *complexityVisitor) TransferNetOwnershipTx(tx *txs.TransferNetOwnershipTx) error {
	c.output = IntrinsicTransferNetOwnershipTxComplexities
	return nil
}
EOF
  fi
  
  # Check if file contains staticVisitor
  if grep -q "staticVisitor" "$file"; then
    cat >> "$file" << 'EOF'

func (v *staticVisitor) AddNetValidatorTx(*txs.AddNetValidatorTx) error {
	v.fee = v.config.AddNetValidatorTxFee
	return nil
}

func (v *staticVisitor) CreateNetTx(*txs.CreateNetTx) error {
	v.fee = v.config.CreateNetTxFee
	return nil
}

func (v *staticVisitor) RemoveNetValidatorTx(*txs.RemoveNetValidatorTx) error {
	v.fee = v.config.TxFee
	return nil
}

func (v *staticVisitor) TransformNetTx(*txs.TransformNetTx) error {
	v.fee = v.config.TransformNetTxFee
	return nil
}

func (v *staticVisitor) TransferNetOwnershipTx(*txs.TransferNetOwnershipTx) error {
	v.fee = v.config.TxFee
	return nil
}
EOF
  fi
  
  # Check if file is signer visitor
  if grep -q "type visitor struct" "$file" && grep -q "signer" "$file"; then
    cat >> "$file" << 'EOF'

func (v *visitor) AddNetValidatorTx(tx *txs.AddNetValidatorTx) error {
	txSigners, err := v.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	if err := sign(v.tx, false, txSigners); err != nil {
		return err
	}
	return sign(v.tx, true, v.subnetSigners)
}

func (v *visitor) CreateNetTx(tx *txs.CreateNetTx) error {
	txSigners, err := v.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(v.tx, false, txSigners)
}

func (v *visitor) RemoveNetValidatorTx(tx *txs.RemoveNetValidatorTx) error {
	txSigners, err := v.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	if err := sign(v.tx, false, txSigners); err != nil {
		return err
	}
	return sign(v.tx, true, v.subnetSigners)
}

func (v *visitor) TransformNetTx(tx *txs.TransformNetTx) error {
	txSigners, err := v.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	if err := sign(v.tx, false, txSigners); err != nil {
		return err
	}
	return sign(v.tx, true, v.subnetSigners)
}

func (v *visitor) TransferNetOwnershipTx(tx *txs.TransferNetOwnershipTx) error {
	txSigners, err := v.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	if err := sign(v.tx, false, txSigners); err != nil {
		return err
	}
	return sign(v.tx, true, v.subnetSigners)
}
EOF
  fi
  
  # Check if file is tx_metrics
  if grep -q "txMetrics" "$file"; then
    cat >> "$file" << 'EOF'

func (m *txMetrics) AddNetValidatorTx(*txs.AddNetValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_net_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) CreateNetTx(*txs.CreateNetTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "create_net",
	}).Inc()
	return nil
}

func (m *txMetrics) RemoveNetValidatorTx(*txs.RemoveNetValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "remove_net_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) TransformNetTx(*txs.TransformNetTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "transform_net",
	}).Inc()
	return nil
}

func (m *txMetrics) TransferNetOwnershipTx(*txs.TransferNetOwnershipTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "transfer_net_ownership",
	}).Inc()
	return nil
}
EOF
  fi
done

echo "Visitor implementations updated!"